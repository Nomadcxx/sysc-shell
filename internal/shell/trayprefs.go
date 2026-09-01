package shell

import (
	"sort"
	"strings"
	"unicode"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

type trayPreferenceEdit uint8

const (
	trayPreferenceHide trayPreferenceEdit = iota
	trayPreferenceShow
	trayPreferencePin
	trayPreferenceUnpin
	trayPreferenceEarlier
	trayPreferenceLater
)

type trayArrangement struct {
	Bar        []tray.Item
	Overflow   []tray.Item
	Hidden     []tray.Item
	Pinned     map[tray.ItemKey]bool
	Collisions []string
}

// stableTrayToken deliberately excludes the service generation. Some SNI
// implementations publish the interface name as their ID/title; that value
// identifies the protocol, not the application, so it cannot safely own a
// persisted preference.
func stableTrayToken(item tray.Item) (string, bool) {
	if value := strings.TrimSpace(item.ID); !genericTrayIdentity(value) {
		return "id:" + value, true
	}
	if value := strings.TrimSpace(item.Title); !genericTrayIdentity(value) {
		return "title:" + value, true
	}
	return "", false
}

func genericTrayIdentity(value string) bool {
	if value == "" {
		return true
	}
	var compact strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact.WriteRune(r)
		}
	}
	return compact.String() == "statusnotifieritem" || compact.String() == "orgkdestatusnotifieritem"
}

// arrangeTray is the output-independent preference projection. available is
// the logical width granted to tray icons; geometry, not an item-count option,
// decides which visible items move into overflow.
func arrangeTray(items []tray.Item, prefs config.TrayPreferences, available, itemSize, spacing int) trayArrangement {
	tokens := make([]string, len(items))
	counts := make(map[string]int, len(items))
	for i, item := range items {
		if token, ok := stableTrayToken(item); ok {
			tokens[i] = token
			counts[token]++
		}
	}
	var out trayArrangement
	seenCollision := map[string]bool{}
	type candidate struct {
		item   tray.Item
		token  string
		pinned bool
		index  int
	}
	visible := make([]candidate, 0, len(items))
	hidden := stringSet(prefs.Hidden)
	pinned := stringSet(prefs.Pinned)
	for i, item := range items {
		if item.Status == tray.StatusPassive {
			continue
		}
		token := tokens[i]
		if token != "" && counts[token] > 1 {
			if !seenCollision[token] {
				out.Collisions = append(out.Collisions, token)
				seenCollision[token] = true
			}
			token = "" // no preference applies to either colliding item
		}
		if token != "" && hidden[token] {
			out.Hidden = append(out.Hidden, item)
			continue
		}
		visible = append(visible, candidate{item: item, token: token, pinned: token != "" && pinned[token], index: i})
	}

	rank := make(map[string]int, len(prefs.Order))
	for i, token := range prefs.Order {
		rank[token] = i
	}
	sort.SliceStable(visible, func(i, j int) bool {
		a, aok := rank[visible[i].token]
		b, bok := rank[visible[j].token]
		switch {
		case aok && bok:
			return a < b
		case aok:
			return true
		case bok:
			return false
		default:
			return visible[i].index < visible[j].index
		}
	})
	ordered := make([]tray.Item, 0, len(visible))
	for _, wantPinned := range []bool{true, false} {
		for _, c := range visible {
			if c.pinned == wantPinned {
				ordered = append(ordered, c.item)
				if c.pinned {
					if out.Pinned == nil {
						out.Pinned = make(map[tray.ItemKey]bool)
					}
					out.Pinned[c.item.Key] = true
				}
			}
		}
	}

	slots := traySlots(available, itemSize, spacing)
	if slots > len(ordered) {
		slots = len(ordered)
	}
	out.Bar = append(out.Bar, ordered[:slots]...)
	out.Overflow = append(out.Overflow, ordered[slots:]...)
	return out
}

func traySlots(available, itemSize, spacing int) int {
	if available <= 0 || itemSize <= 0 {
		return 0
	}
	if spacing < 0 {
		spacing = 0
	}
	return (available + spacing) / (itemSize + spacing)
}

func editTrayPreferences(p config.TrayPreferences, edit trayPreferenceEdit, token string, liveOrder []string) config.TrayPreferences {
	p.Hidden = append([]string(nil), p.Hidden...)
	p.Pinned = append([]string(nil), p.Pinned...)
	p.Order = append([]string(nil), p.Order...)
	if token == "" {
		return p
	}
	switch edit {
	case trayPreferenceHide:
		p.Hidden = addUnique(p.Hidden, token)
	case trayPreferenceShow:
		p.Hidden = removeString(p.Hidden, token)
	case trayPreferencePin:
		p.Pinned = addUnique(p.Pinned, token)
	case trayPreferenceUnpin:
		p.Pinned = removeString(p.Pinned, token)
	case trayPreferenceEarlier, trayPreferenceLater:
		p.Order = materializeTrayOrder(liveOrder, p.Order)
		p.Order = addUnique(p.Order, token)
		i := indexString(p.Order, token)
		j := i - 1
		if edit == trayPreferenceLater {
			j = i + 1
		}
		if i >= 0 && j >= 0 && j < len(p.Order) {
			p.Order[i], p.Order[j] = p.Order[j], p.Order[i]
		}
	}
	return p
}

// materializeTrayOrder makes adjacent edits operate on the order the user can
// currently see. Saved tokens for absent items stay at the end so temporarily
// stopped services do not lose their preference.
func materializeTrayOrder(live, saved []string) []string {
	out := make([]string, 0, len(live)+len(saved))
	for _, token := range append(append([]string(nil), live...), saved...) {
		if token != "" {
			out = addUnique(out, token)
		}
	}
	return out
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func addUnique(values []string, value string) []string {
	if indexString(values, value) >= 0 {
		return values
	}
	return append(values, value)
}

func removeString(values []string, value string) []string {
	if i := indexString(values, value); i >= 0 {
		return append(values[:i], values[i+1:]...)
	}
	return values
}

func indexString(values []string, value string) int {
	for i := range values {
		if values[i] == value {
			return i
		}
	}
	return -1
}
