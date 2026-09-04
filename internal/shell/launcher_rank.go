package shell

import (
	"sort"
	"strings"
	"sync"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// The launcher has two modes wearing one code path. A non-empty query is a
// *search*: the fuzzy scores are meaningful, they separate the entries, and
// past the first screenful the tail is noise -- capping it is right. An empty
// query is a *browse*: every entry scores 0, the sort collapses to
// alphabetical, and a cap silently amputates the list at a letter. sysc-launch
// caps both, so browsing 482 desktop entries showed 50 and stopped in the E's.
//
// launcherRank is injected as launcher.ServiceConfig.Rank to cap the search
// and leave the browse whole. Everything else -- the field weighting, the
// boost cap, the tie-break chain -- is copied from sysc-launch's own rank so
// search order does not drift between the two. TestLauncherRankMatchesLibrary
// pins that parity against the library itself.
const (
	launcherSearchLimit   = 50
	launcherUsageBoostCap = 25
)

var launcherInitFZF sync.Once

func launcherRank(entries []launcher.Entry, query string, boost func(query, identifier string) int) []launcher.Result {
	query = strings.TrimSpace(query)
	slab := util.MakeSlab(100*1024, 2048)
	results := make([]launcher.Result, 0, len(entries))
	for _, entry := range entries {
		score, matched := launcherEntryScore(entry, query, slab)
		if !matched {
			continue
		}
		if boost != nil {
			score += min(boost(query, entry.ID), launcherUsageBoostCap)
		}
		results = append(results, launcher.Result{Entry: entry, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		left, right := strings.ToLower(results[i].Entry.Name), strings.ToLower(results[j].Entry.Name)
		if left != right {
			return left < right
		}
		if results[i].Entry.Name != results[j].Entry.Name {
			return results[i].Entry.Name < results[j].Entry.Name
		}
		return results[i].Entry.ID < results[j].Entry.ID
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
	fields := [...]string{
		entry.Name,
		entry.GenericName,
		strings.Join(entry.Keywords, " "),
		strings.Join(entry.Argv, " "),
		entry.Comment,
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
