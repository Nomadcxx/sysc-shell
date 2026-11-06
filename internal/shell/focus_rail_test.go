package shell

import "testing"

// Rail tabs stack closer than an outward ring reaches, so they take the
// inward ring (sysc-2259).
func TestRailTabsUseInwardFocusRing(t *testing.T) {
	for name, id := range map[string]PanelID{"settings": PanelSettings, "control centre": PanelControlCenter} {
		t.Run(name, func(t *testing.T) {
			r := newPanelRegistry(t)
			if err := r.OpenPanel(id, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			drainAuxQueue(r)
			r.mu.Lock()
			defer r.mu.Unlock()
			tabs := 0
			for _, n := range r.panelHosts[id].focus {
				if n.Role != "tab" || len(n.Action) < 8 || n.Action[:8] != "section:" {
					continue
				}
				tabs++
				if !n.FocusInward {
					t.Errorf("%s tab %q draws an outward ring", name, n.Name)
				}
			}
			if tabs < 10 {
				t.Fatalf("found %d rail tabs, want the full rail", tabs)
			}
		})
	}
}
