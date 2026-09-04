package shell

import (
	"os"
	"path/filepath"
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
		lookup  []runningAppEntry
		want    []runningAppSlot
	}{
		{
			name: "two firefox",
			windows: []niri.Window{
				{ID: 1, AppID: "firefox"},
				{ID: 2, AppID: "firefox"},
			},
			lookup: []runningAppEntry{firefox},
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
			lookup: []runningAppEntry{firefox},
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
			lookup: []runningAppEntry{steam},
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
			lookup: []runningAppEntry{steam, game},
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
			lookup: []runningAppEntry{firefox},
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

func TestLookupRunningApp(t *testing.T) {
	t.Parallel()
	firefox := runningAppEntry{ID: "firefox"}
	steam := runningAppEntry{ID: "steam"}
	foo := runningAppEntry{ID: "org.foo.Bar", StartupWMClass: "Foo"}

	cases := []struct {
		name    string
		appID   string
		entries []runningAppEntry
		wantID  string
		miss    bool
	}{
		{name: "exact", appID: "firefox", entries: []runningAppEntry{firefox}, wantID: "firefox"},
		{name: "fold", appID: "Firefox", entries: []runningAppEntry{firefox}, wantID: "firefox"},
		{name: "tail", appID: "org.mozilla.firefox", entries: []runningAppEntry{firefox}, wantID: "firefox"},
		{name: "wmclass", appID: "Foo", entries: []runningAppEntry{foo}, wantID: "org.foo.Bar"},
		{name: "steam", appID: "steam", entries: []runningAppEntry{steam}, wantID: "steam"},
		{name: "miss", appID: "xyz", entries: []runningAppEntry{firefox}, miss: true},
		{name: "empty", appID: "firefox", miss: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lookupRunningApp(tc.appID, tc.entries)
			if tc.miss {
				if ok {
					t.Fatalf("hit %+v, want miss", got)
				}
				return
			}
			if !ok || got.ID != tc.wantID {
				t.Fatalf("got %+v ok=%v, want id %q", got, ok, tc.wantID)
			}
		})
	}
}

func TestLoadRunningAppEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeDesktop := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktop("firefox.desktop", "[Desktop Entry]\nType=Application\nName=Firefox\nIcon=firefox\nStartupWMClass=Firefox\nActions=NewWindow;\n\n[Desktop Action NewWindow]\nName=New Window\nExec=firefox --new-window\n")
	writeDesktop("ghost.desktop", "[Desktop Entry]\nType=Application\nName=Ghost\nNoDisplay=true\nIcon=ghost\n")
	writeDesktop("gone.desktop", "[Desktop Entry]\nType=Application\nName=Gone\nHidden=true\nIcon=gone\n")

	got := loadRunningAppEntries([]string{dir})
	byID := map[string]runningAppEntry{}
	for _, e := range got {
		byID[e.ID] = e
	}
	if _, ok := byID["gone"]; ok {
		t.Fatal("Hidden tombstone stayed in the index")
	}
	ghost, ok := byID["ghost"]
	if !ok || ghost.Icon != "ghost" {
		t.Fatalf("NoDisplay entry = %+v ok=%v, want kept", ghost, ok)
	}
	ff, ok := byID["firefox"]
	if !ok || ff.Icon != "firefox" || ff.StartupWMClass != "Firefox" {
		t.Fatalf("firefox = %+v ok=%v", ff, ok)
	}
	if len(ff.Actions) != 1 || ff.Actions[0].ID != "NewWindow" || ff.Actions[0].Name != "New Window" {
		t.Fatalf("firefox actions = %+v", ff.Actions)
	}
}

func TestNextFocusID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		slot runningAppSlot
		want uint64
	}{
		{
			name: "unfocused uses MRU",
			slot: runningAppSlot{
				Members: []niri.Window{{ID: 1}, {ID: 2}, {ID: 3}},
				MRU:     niri.Window{ID: 2},
			},
			want: 2,
		},
		{
			name: "focused advances",
			slot: runningAppSlot{
				Focused: true,
				Members: []niri.Window{{ID: 1, Focused: true}, {ID: 2}, {ID: 3}},
			},
			want: 2,
		},
		{
			name: "wrap",
			slot: runningAppSlot{
				Focused: true,
				Members: []niri.Window{{ID: 1}, {ID: 2}, {ID: 3, Focused: true}},
			},
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextFocusID(tc.slot); got != tc.want {
				t.Fatalf("nextFocusID = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunningAppMenu(t *testing.T) {
	t.Parallel()
	steam := runningAppSlot{
		Actions: []runningAppAction{
			{ID: "Store", Name: "Store"},
			{ID: "Library", Name: "Library"},
			{ID: "Friends", Name: "Friends"},
		},
	}
	got := runningAppMenu(steam)
	want := []runningAppMenuRow{
		{Label: "Store", ActionID: "Store"},
		{Label: "Library", ActionID: "Library"},
		{Label: "Friends", ActionID: "Friends"},
		{Label: "Close all", CloseAll: true},
	}
	if len(got) != len(want) {
		t.Fatalf("menu = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	only := runningAppMenu(runningAppSlot{})
	if len(only) != 1 || !only[0].CloseAll || only[0].Label != "Close all" {
		t.Fatalf("empty actions = %+v, want Close all only", only)
	}
}
