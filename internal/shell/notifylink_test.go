package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestNotificationCentreKeepsALinkReachable proves the anchor in a card body
// survives into the notification centre's tree and into the focus order the
// panel resolves a pointer press against. A link that only carried an action
// was drawn but could never be reached.
func TestNotificationCentreKeepsALinkReachable(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	n := note(7, "active")
	n.Body = `read <a href="https://example.test/a">the page</a>`
	r.applyNotify(snap(1, n))

	h := &PanelHost{id: PanelNotifications}
	tree := r.centerTreeFor(h)
	link := findAction(tree, "notify:7:link:https://example.test/a")
	if link == nil {
		t.Fatal("the notification centre carries no link")
	}
	if !link.Focusable {
		t.Fatal("the link is not focusable, so a pointer press cannot reach it")
	}
	if link.Role != "link" || link.Name == "" {
		t.Fatalf("link role=%q name=%q, want a named link", link.Role, link.Name)
	}
	found := false
	for _, f := range ui.Focusables(tree) {
		if f == link {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the link is not in the panel focus order")
	}
}

// TestPanelActivateOpensANotificationLink covers the issue's acceptance
// criterion: a supported HTTP or HTTPS link opens, and nothing else does.
func TestPanelActivateOpensANotificationLink(t *testing.T) {
	cases := []struct {
		name string
		href string
		want bool
	}{
		{name: "http", href: "http://example.test/a", want: true},
		{name: "https", href: "https://example.test/a", want: true},
		{name: "file", href: "file:///etc/passwd"},
		{name: "opaque", href: "mailto:someone@example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry(config.Default())
			t.Cleanup(r.Close)
			sender := &fakeNotifySender{}
			r.notifySender = sender
			opened := make(chan string, 1)
			t.Cleanup(openNotificationURL.Swap(func(href string) error {
				opened <- href
				return nil
			}))

			h := &PanelHost{id: PanelNotifications}
			if !h.activateNotify(r, &ui.Node{Action: "notify:7:link:" + tc.href}) {
				t.Fatal("the link action was not handled")
			}
			wait := time.Second
			if !tc.want {
				wait = 100 * time.Millisecond
			}
			select {
			case got := <-opened:
				if !tc.want {
					t.Fatalf("opened %q, want the link refused", got)
				}
				if got != tc.href {
					t.Fatalf("opened %q, want %q", got, tc.href)
				}
			case <-time.After(wait):
				if tc.want {
					t.Fatal("the link did not open")
				}
			}
			if len(sender.cmds) != 0 {
				t.Fatalf("a link issued notification commands: %+v", sender.cmds)
			}
		})
	}
}

// TestPanelLinkOpensAfterTheRegistryUnlock proves the desktop opener does not
// start while the panel handler still holds the registry lock: the opener
// would otherwise wait behind the very handler that asked for it.
func TestPanelLinkOpensAfterTheRegistryUnlock(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	started := make(chan bool, 1)
	t.Cleanup(openNotificationURL.Swap(func(string) error {
		acquired := r.mu.TryLock()
		if acquired {
			r.mu.Unlock()
		}
		started <- acquired
		return nil
	}))

	h := &PanelHost{id: PanelNotifications}
	r.mu.Lock()
	h.activateNotify(r, &ui.Node{Action: "notify:7:link:https://example.test/a"})
	select {
	case <-started:
		t.Fatal("the desktop opener started while the panel handler held Registry.mu")
	default:
	}
	r.mu.Unlock()

	select {
	case acquired := <-started:
		if !acquired {
			t.Fatal("the desktop opener started before Registry.mu was released")
		}
	case <-time.After(time.Second):
		t.Fatal("the desktop opener did not start")
	}
}
