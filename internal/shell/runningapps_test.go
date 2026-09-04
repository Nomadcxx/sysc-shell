package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

func TestGroupRunningApps(t *testing.T) {
	t.Parallel()
	firefox := runningAppEntry{ID: "firefox", Icon: "firefox"}
	steam := runningAppEntry{ID: "steam", Icon: "steam"}
	game := runningAppEntry{ID: "steam_app_123", Icon: "game"}

	cases := []struct {
		name    string
		windows []niri.Window
		lookup  map[string]runningAppEntry
		want    []runningAppSlot
	}{
		{
			name: "two firefox",
			windows: []niri.Window{
				{ID: 1, AppID: "firefox"},
				{ID: 2, AppID: "firefox"},
			},
			lookup: map[string]runningAppEntry{"firefox": firefox},
			want: []runningAppSlot{{
				Key:     "firefox",
				Icon:    "firefox",
				Members: []niri.Window{{ID: 1, AppID: "firefox"}, {ID: 2, AppID: "firefox"}},
			}},
		},
		{
			name: "firefox then brave",
			windows: []niri.Window{
				{ID: 1, AppID: "firefox"},
				{ID: 2, AppID: "brave"},
			},
			lookup: map[string]runningAppEntry{"firefox": firefox},
			want: []runningAppSlot{
				{Key: "firefox", Icon: "firefox", Members: []niri.Window{{ID: 1, AppID: "firefox"}}},
				{Key: "brave", Members: []niri.Window{{ID: 2, AppID: "brave"}}},
			},
		},
		{
			name: "steam_app folds into steam",
			windows: []niri.Window{
				{ID: 1, AppID: "steam_app_123"},
				{ID: 2, AppID: "steam"},
			},
			lookup: map[string]runningAppEntry{"steam": steam},
			want: []runningAppSlot{{
				Key:     "steam",
				Icon:    "steam",
				Members: []niri.Window{{ID: 1, AppID: "steam_app_123"}, {ID: 2, AppID: "steam"}},
			}},
		},
		{
			name: "steam_app with its own desktop file",
			windows: []niri.Window{
				{ID: 1, AppID: "steam_app_123"},
				{ID: 2, AppID: "steam"},
			},
			lookup: map[string]runningAppEntry{"steam": steam, "steam_app_123": game},
			want: []runningAppSlot{
				{Key: "steam_app_123", Icon: "game", Members: []niri.Window{{ID: 1, AppID: "steam_app_123"}}},
				{Key: "steam", Icon: "steam", Members: []niri.Window{{ID: 2, AppID: "steam"}}},
			},
		},
		{
			name:    "unknown",
			windows: []niri.Window{{ID: 9, AppID: "xyz"}},
			want:    []runningAppSlot{{Key: "xyz", Members: []niri.Window{{ID: 9, AppID: "xyz"}}}},
		},
		{
			name: "empty",
		},
		{
			name: "focused MRU",
			windows: []niri.Window{
				{ID: 1, AppID: "firefox", FocusTimestamp: 10},
				{ID: 2, AppID: "firefox", Focused: true, FocusTimestamp: 20},
			},
			lookup: map[string]runningAppEntry{"firefox": firefox},
			want: []runningAppSlot{{
				Key:     "firefox",
				Icon:    "firefox",
				Focused: true,
				Members: []niri.Window{
					{ID: 1, AppID: "firefox", FocusTimestamp: 10},
					{ID: 2, AppID: "firefox", Focused: true, FocusTimestamp: 20},
				},
				MRU: niri.Window{ID: 2, AppID: "firefox", Focused: true, FocusTimestamp: 20},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := groupRunningApps(tc.windows, tc.lookup)
			if len(got) != len(tc.want) {
				t.Fatalf("slots = %d, want %d (%+v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i].Key != tc.want[i].Key || got[i].Icon != tc.want[i].Icon || got[i].Focused != tc.want[i].Focused {
					t.Fatalf("slot %d = %+v, want %+v", i, got[i], tc.want[i])
				}
				if len(got[i].Members) != len(tc.want[i].Members) {
					t.Fatalf("slot %d members = %d, want %d", i, len(got[i].Members), len(tc.want[i].Members))
				}
				for j, m := range tc.want[i].Members {
					if got[i].Members[j].ID != m.ID {
						t.Fatalf("slot %d member %d id = %d, want %d", i, j, got[i].Members[j].ID, m.ID)
					}
				}
				if tc.want[i].MRU.ID != 0 && got[i].MRU.ID != tc.want[i].MRU.ID {
					t.Fatalf("slot %d MRU = %d, want %d", i, got[i].MRU.ID, tc.want[i].MRU.ID)
				}
			}
		})
	}
}
