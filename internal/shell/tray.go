package shell

import (
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// trayState is the shell's projection of service-owned tray items. Items are
// keyed by their full ItemKey — owner, path, and generation — so a
// re-registered owner never aliases its replacement and a stale command never
// reaches it.
type trayState struct {
	mu         sync.Mutex
	generation uint64
	items      map[tray.ItemKey]tray.Item
	order      []tray.ItemKey
	menus      map[tray.ItemKey]tray.Menu
}

func newTrayState() *trayState {
	return &trayState{items: map[tray.ItemKey]tray.Item{}, menus: map[tray.ItemKey]tray.Menu{}}
}

// applyTray applies one immutable client message. A stale generation is
// discarded; a disconnect drops everything.
func (s *trayState) applyTray(m trayclient.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch m.Kind {
	case trayclient.KindSnapshot:
		s.generation = m.Generation
		s.items = make(map[tray.ItemKey]tray.Item, len(m.Snapshot.Items))
		s.order = s.order[:0]
		for _, it := range m.Snapshot.Items {
			s.items[it.Key] = it
			s.order = append(s.order, it.Key)
		}
	case trayclient.KindItemAdded, trayclient.KindItemChanged:
		if m.Generation != s.generation {
			return
		}
		if _, exists := s.items[m.Item.Key]; !exists {
			s.order = append(s.order, m.Item.Key)
		}
		s.items[m.Item.Key] = m.Item
	case trayclient.KindItemRemoved:
		if m.Generation != s.generation {
			return
		}
		delete(s.items, m.Removed.Key)
		delete(s.menus, m.Removed.Key)
		for i, key := range s.order {
			if key == m.Removed.Key {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	case trayclient.KindMenuUpdated:
		if m.Generation != s.generation {
			return
		}
		s.menus[m.Menu.Key] = m.Menu.Menu
	case trayclient.KindDisconnected:
		s.generation = 0
		s.items = map[tray.ItemKey]tray.Item{}
		s.order = nil
		s.menus = map[tray.ItemKey]tray.Menu{}
	}
}

func (s *trayState) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

func (s *trayState) title(key tray.ItemKey) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[key].Title
}

// iconName resolves the effective named icon: NeedsAttention replaces the
// normal icon with the attention icon.
func (s *trayState) iconName(key tray.ItemKey) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return "", false
	}
	if it.Status == tray.StatusNeedsAttention && it.AttentionIcon.Name != "" {
		return it.AttentionIcon.Name, true
	}
	return it.Icon.Name, it.Icon.Name != ""
}

// items returns the projection for one output. Projection is
// output-independent: every output reads the same set.
func (s *trayState) itemsList() []tray.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tray.Item, 0, len(s.items))
	for _, key := range s.order {
		if it, ok := s.items[key]; ok {
			out = append(out, it)
		}
	}
	return out
}

// has reports whether the exact key — owner, path, generation — is live.
func (s *trayState) has(key tray.ItemKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[key]
	return ok
}

// menuFor returns the service-owned menu for one item, when one has arrived.
func (s *trayState) menuFor(key tray.ItemKey) (tray.Menu, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.menus[key]
	return m, ok
}

// tooltipText flattens the service-owned tooltip into one dwell string. The
// service already bounded the fields; the shell clamps again at the protocol
// bound so a compromised service cannot grow the bar's hover surface.
func (s *trayState) tooltipText(key tray.ItemKey) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return ""
	}
	text := it.Tooltip.Title
	if it.Tooltip.Description != "" {
		if text != "" {
			text += "\n"
		}
		text += it.Tooltip.Description
	}
	if len(text) > tray.MaxTooltipBytes {
		text = text[:tray.MaxTooltipBytes]
	}
	return text
}

// Registry wrappers.
func (r *Registry) applyTray(m trayclient.Message)             { r.tray.applyTray(m) }
func (r *Registry) trayItemCount() int                         { return r.tray.count() }
func (r *Registry) trayTitle(k tray.ItemKey) string            { return r.tray.title(k) }
func (r *Registry) trayIconName(k tray.ItemKey) (string, bool) { return r.tray.iconName(k) }
func (r *Registry) trayItemsFor(string) []tray.Item            { return r.tray.itemsList() }
func (r *Registry) trayTooltipText(k tray.ItemKey) string      { return r.tray.tooltipText(k) }
