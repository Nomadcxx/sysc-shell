package shell

import (
	"os"
	"path/filepath"
	"slices"
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
				Key:          "steam",
				Icon:         "steam",
				Members:      []niri.Window{{ID: 1, AppID: "steam_app_123"}, {ID: 2, AppID: "steam"}},
				SteamGameIDs: map[uint64]struct{}{1: {}},
			}},
		},
		{
			name: "steam_application does not fold into steam",
			windows: []niri.Window{
				{ID: 1, AppID: "steam_application"},
				{ID: 2, AppID: "steam"},
			},
			lookup: []runningAppEntry{steam},
			want: []runningAppSlot{
				{Key: "steam_application", Members: []niri.Window{{ID: 1, AppID: "steam_application"}}},
				{Key: "steam", Icon: "steam", Members: []niri.Window{{ID: 2, AppID: "steam"}}},
			},
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
				if len(got[i].SteamGameIDs) != len(tc.want[i].SteamGameIDs) {
					t.Fatalf("slot %d folded Steam game IDs = %v, want %v", i, got[i].SteamGameIDs, tc.want[i].SteamGameIDs)
				}
				for id := range tc.want[i].SteamGameIDs {
					if _, ok := got[i].SteamGameIDs[id]; !ok {
						t.Fatalf("slot %d folded Steam game IDs = %v, want %v", i, got[i].SteamGameIDs, tc.want[i].SteamGameIDs)
					}
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

func TestRunningAppMRUTiesUseStableWindowIDOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		windows []niri.Window
		want    uint64
	}{
		{
			name: "equal nonzero timestamps",
			windows: []niri.Window{
				{ID: 8, AppID: "steam", FocusTimestamp: 10},
				{ID: 3, AppID: "steam", FocusTimestamp: 10},
			},
			want: 3,
		},
		{
			name: "zero timestamps",
			windows: []niri.Window{
				{ID: 8, AppID: "steam"},
				{ID: 3, AppID: "steam"},
			},
			want: 3,
		},
		{
			name: "nonzero outranks zero",
			windows: []niri.Window{
				{ID: 3, AppID: "steam"},
				{ID: 8, AppID: "steam", FocusTimestamp: 1},
			},
			want: 8,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := groupRunningApps(tc.windows, nil)[0].MRU.ID
			if got != tc.want {
				t.Fatalf("MRU ID = %d, want %d", got, tc.want)
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
		Name: "Steam",
		Actions: []runningAppAction{
			{ID: "Store", Name: "Store"},
			{ID: "Library", Name: "Library"},
			{ID: "Friends", Name: "Friends"},
		},
	}
	got := runningAppMenu(steam, nil)
	if gotLabels := runningAppMenuLabels(got); !slices.Equal(gotLabels, []string{
		"Steam (0 windows)", "Store", "Library", "Friends",
	}) {
		t.Fatalf("menu = %v, want header then desktop actions", gotLabels)
	}
	only := runningAppMenu(runningAppSlot{}, nil)
	if len(only) != 0 {
		t.Fatalf("empty slot = %+v, want no menu rows", only)
	}
}

func TestRunningAppMenuProjectsWindowRowsAndMRUWorkspaceTarget(t *testing.T) {
	t.Parallel()
	slot := runningAppSlot{
		Key: "mail", Name: "Mail",
		Members: []niri.Window{
			{ID: 12, AppID: "mail", Title: "Draft", WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20, Focused: true},
			{ID: 11, AppID: "mail", Title: "Inbox", WorkspaceID: 20, HasWorkspace: true, FocusTimestamp: 10},
		},
		MRU:     niri.Window{ID: 12, AppID: "mail", Title: "Draft", WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20, Focused: true},
		Actions: []runningAppAction{{ID: "NewWindow", Name: "New window"}},
	}
	workspaces := []niri.Workspace{
		{ID: 10, Index: 1, Name: "Current"},
		{ID: 20, Index: 2, Name: "Research"},
	}

	rows := runningAppMenu(slot, workspaces)
	want := []string{
		"Mail (2 windows)", "Close all and force quit", "Draft — Current", "Move Draft to floating",
		"Inbox — Research", "Move Inbox to floating",
		"New window", "Move Draft to Research",
	}
	if got := runningAppMenuLabels(rows); !slices.Equal(got, want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
	var move runningAppMenuRow
	for _, row := range rows {
		if row.MoveWindowID != 0 {
			move = row
		}
	}
	if move.MoveWindowID != 12 || move.MoveWorkspaceID != 20 {
		t.Fatalf("move = %+v, want MRU window 12 to workspace 20", move)
	}
}

func TestRunningAppMenuUsesUntitledFallbackAndSingularClose(t *testing.T) {
	t.Parallel()
	slot := runningAppSlot{
		Key: "terminal", Name: "Terminal",
		Members: []niri.Window{{ID: 50, AppID: "terminal"}},
		MRU:     niri.Window{ID: 50, AppID: "terminal"},
	}
	rows := runningAppMenu(slot, nil)
	want := []string{"Terminal (1 window)", "Close all and force quit", "Untitled window", "Move Untitled window to floating"}
	if got := runningAppMenuLabels(rows); !slices.Equal(got, want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
}

func TestRunningAppMenuGroupsFoldedSteamGamesAndExcludesThemFromClose(t *testing.T) {
	t.Parallel()
	slot := runningAppSlot{
		Key: "steam", Name: "Steam",
		SteamGameIDs: map[uint64]struct{}{81: {}, 82: {}},
		Members: []niri.Window{
			{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 30},
			{ID: 81, AppID: "steam_app_123", Title: "Portal", FocusTimestamp: 20},
			{ID: 82, AppID: "steam_app456", Title: "Half-Life", FocusTimestamp: 10},
		},
		MRU: niri.Window{ID: 80, AppID: "steam", Title: "Store", FocusTimestamp: 30},
	}
	rows := runningAppMenu(slot, nil)
	want := []string{
		"Steam (3 windows)", "Close all and force quit", "Store", "Move Store to floating",
		"Games", "Portal", "Move Portal to floating", "Half-Life", "Move Half-Life to floating",
	}
	if got := runningAppMenuLabels(rows); !slices.Equal(got, want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
}

func runningAppMenuLabels(rows []runningAppMenuRow) []string {
	labels := make([]string, len(rows))
	for i, row := range rows {
		labels[i] = row.Label
	}
	return labels
}

func TestRunningAppMenuWorkspaceDestinationsPinTheMRUWindow(t *testing.T) {
	t.Parallel()
	slot := runningAppSlot{MRU: niri.Window{ID: 77, HasWorkspace: true, WorkspaceID: 10}}
	workspaces := []niri.Workspace{
		{ID: 10, Index: 1, Name: "Current", Output: "DP-1"},
		{ID: 20, Index: 2, Name: "Build", Output: "DP-1"},
		{ID: 30, Index: 3, Output: "DP-2"},
	}
	rows := runningAppMenu(slot, workspaces)
	var moves []runningAppMenuRow
	for _, row := range rows {
		if row.MoveWindowID != 0 {
			moves = append(moves, row)
		}
	}
	if len(moves) != 2 {
		t.Fatalf("workspace rows = %+v, want two destinations other than workspace 10", moves)
	}
	if moves[0].MoveWindowID != 77 || moves[0].MoveWorkspaceID != 20 || moves[1].MoveWindowID != 77 || moves[1].MoveWorkspaceID != 30 {
		t.Fatalf("workspace rows = %+v, want MRU window 77 to workspaces 20 and 30", moves)
	}
}

func TestRunningAppSlotsCarryTheDesktopName(t *testing.T) {
	entries := []runningAppEntry{{ID: "org.mozilla.firefox", Name: "Firefox", Icon: "firefox"}}
	slots := groupRunningApps([]niri.Window{
		{ID: 1, AppID: "org.mozilla.firefox", Pid: 100},
		{ID: 2, AppID: "foot", Pid: 200},
	}, entries)
	if slots[0].Name != "Firefox" {
		t.Errorf("matched slot name = %q, want Firefox", slots[0].Name)
	}
	if slots[1].Name != "foot" {
		t.Errorf("unmatched slot name = %q, want the app id", slots[1].Name)
	}
}

func TestLoadRunningAppEntriesReadsTheName(t *testing.T) {
	dir := t.TempDir()
	body := "[Desktop Entry]\nType=Application\nName=Firefox\nIcon=firefox\nExec=firefox\n"
	if err := os.WriteFile(filepath.Join(dir, "firefox.desktop"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := loadRunningAppEntries([]string{dir})
	if len(entries) != 1 || entries[0].Name != "Firefox" {
		t.Fatalf("entries = %+v", entries)
	}
}
