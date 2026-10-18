package shell

import (
	"sort"
	"strings"
	"sync"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// launcherRank keeps the library's application scoring and ordering, adds
// desktop actions for searches, and caps search results at 50. Browse keeps
// every application. TestLauncherRankMatchesLibraryForSearches guards parity.
const (
	launcherSearchLimit   = 50
	launcherUsageBoostCap = 25
	// launcherActionPenalty keeps an application above its own desktop
	// actions when the same text matches both.
	launcherActionPenalty = 5
)

var launcherInitFZF sync.Once

type launcherAppPreferenceSnapshot struct {
	favourites        map[string]struct{}
	hidden            map[string]struct{}
	defaultTerminalID string
}

func newLauncherAppPreferenceSnapshot(prefs config.ApplicationPreferences) *launcherAppPreferenceSnapshot {
	snapshot := &launcherAppPreferenceSnapshot{
		favourites:        make(map[string]struct{}, len(prefs.Favourites)),
		hidden:            make(map[string]struct{}, len(prefs.Hidden)),
		defaultTerminalID: prefs.DefaultTerminal,
	}
	for _, id := range prefs.Favourites {
		snapshot.favourites[id] = struct{}{}
	}
	for _, id := range prefs.Hidden {
		snapshot.hidden[id] = struct{}{}
	}
	return snapshot
}

func (p *launcherAppPreferenceSnapshot) hides(id string) bool {
	if p == nil {
		return false
	}
	_, hidden := p.hidden[id]
	return hidden
}

func (p *launcherAppPreferenceSnapshot) favours(id string) bool {
	if p == nil {
		return false
	}
	_, favourite := p.favourites[id]
	return favourite
}

func launcherRank(entries []launcher.Entry, query string, boost func(query, identifier string) int) []launcher.Result {
	return launcherRankWithPreferences(entries, query, boost, nil)
}

func launcherRankWithPreferences(entries []launcher.Entry, query string, boost func(query, identifier string) int, prefs *launcherAppPreferenceSnapshot) []launcher.Result {
	query = strings.TrimSpace(query)
	slab := util.MakeSlab(100*1024, 2048)
	results := make([]launcher.Result, 0, len(entries))
	for _, entry := range entries {
		if prefs.hides(entry.ID) {
			continue
		}
		score, matched := launcherEntryScore(entry, query, slab)
		if !matched {
			continue
		}
		if boost != nil {
			score += min(boost(query, entry.ID), launcherUsageBoostCap)
		}
		results = append(results, launcher.Result{Entry: entry, Score: score})
	}
	// Desktop actions are rows of their own when searching; browse stays
	// applications only. An action matches on "<App> <Action>".
	if query != "" {
		for _, entry := range entries {
			if prefs.hides(entry.ID) {
				continue
			}
			for _, a := range entry.Actions {
				score, ok := launcherFuzzyScore(entry.Name+" "+a.Name, query, slab)
				if !ok {
					continue
				}
				if boost != nil {
					score += min(boost(query, entry.ID), launcherUsageBoostCap)
				}
				row := entry
				row.Name = entry.Name + " · " + a.Name
				row.Comment = "Desktop action"
				if a.IconName != "" {
					row.IconName = a.IconName
				}
				results = append(results, launcher.Result{Entry: row, Score: score - launcherActionPenalty, Action: a.ID})
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if left, right := prefs.favours(results[i].Entry.ID), prefs.favours(results[j].Entry.ID); left != right {
			return left
		}
		left, right := strings.ToLower(results[i].Entry.Name), strings.ToLower(results[j].Entry.Name)
		if left != right {
			return left < right
		}
		if results[i].Entry.Name != results[j].Entry.Name {
			return results[i].Entry.Name < results[j].Entry.Name
		}
		if results[i].Entry.ID != results[j].Entry.ID {
			return results[i].Entry.ID < results[j].Entry.ID
		}
		return results[i].Action < results[j].Action // an action row shares its app's ID
	})
	if query != "" && len(results) > launcherSearchLimit {
		results = results[:launcherSearchLimit]
	}
	return results
}

// launcherEntryScore is sysc-launch's entryScore: the best fuzzy score across
// the entry's fields, each field past the name penalised by its position.
func launcherEntryScore(entry launcher.Entry, query string, slab *util.Slab) (int, bool) {
	if query == "" {
		return 0, true
	}
	exec := entry.ExecArgv
	if len(exec) == 0 {
		exec = entry.Argv
	}
	fields := [...]string{
		entry.Name,
		entry.GenericName,
		strings.Join(entry.Keywords, " "),
		strings.Join(exec, " "),
		entry.Comment,
		strings.Join(entry.Categories, " "),
	}
	best, matched := 0, false
	for i, field := range fields {
		raw, ok := launcherFuzzyScore(field, query, slab)
		if !ok {
			continue
		}
		score := raw - min(i*5, 50)
		if !matched || score > best {
			best, matched = score, true
		}
	}
	return best, matched
}

func launcherFuzzyScore(candidate, query string, slab *util.Slab) (int, bool) {
	if candidate == "" || query == "" {
		return 0, false
	}
	launcherInitFZF.Do(func() { algo.Init("default") })
	chars := util.ToChars([]byte(candidate))
	result, _ := algo.FuzzyMatchV2(false, true, true, &chars, []rune(strings.ToLower(query)), false, slab)
	return result.Score, result.Start >= 0
}
