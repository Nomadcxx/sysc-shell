package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestTheDefaultBarContainsNotifications(t *testing.T) {
	t.Parallel()
	right := config.Default().Bar.Right
	if len(right) == 0 || right[len(right)-1].ID != "notifications" {
		t.Fatalf("right = %+v, want notifications after battery", right)
	}
}

func TestNotificationsParsesAsAKnownItem(t *testing.T) {
	t.Parallel()
	cfg, err := config.Parse([]byte(`{"bar":{"items":{"right":["notifications"]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Bar.Right[0].ID != "notifications" {
		t.Fatalf("id = %q", cfg.Bar.Right[0].ID)
	}
}

func TestANotifyWidgetRendersOneFixedMaterialBell(t *testing.T) {
	t.Parallel()
	w := notifyWidget(t)
	w.refresh(barView{})
	if w.inner.Kind != ui.KindIcon || w.inner.Icon != "notifications" {
		t.Fatalf("indicator = kind %v icon %q, want one notifications icon", w.inner.Kind, w.inner.Icon)
	}
	if w.inner.IconSize != 20 {
		t.Fatalf("icon size = %d, want 20", w.inner.IconSize)
	}
	if len(w.inner.Children) != 0 {
		t.Fatalf("bell has %d children, want no width-changing sibling", len(w.inner.Children))
	}
}

func TestUnreadUsesAnErrorBadgeWithoutChangingThePillWidth(t *testing.T) {
	t.Parallel()
	width := func(v barView) (int, ui.Fill) {
		w := notifyWidget(t)
		w.refresh(v)
		root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{w.node}}
		if err := ui.Layout(root, ui.Rect{W: 100, H: 48}, func(string, ui.TextAttrs) (int, int) {
			return 8, 16
		}); err != nil {
			t.Fatal(err)
		}
		return w.node.Bounds.W, w.inner.Fill
	}
	readW, readFill := width(barView{})
	unreadW, unreadFill := width(barView{Unread: 3})
	if readW != unreadW {
		t.Fatalf("pill width changed from %d to %d for unread", readW, unreadW)
	}
	if readFill != ui.FillNone || unreadFill != ui.FillError {
		t.Fatalf("badge fills = read %v unread %v, want none/error", readFill, unreadFill)
	}
}

func TestDNDSwapsToMaterialDoNotDisturb(t *testing.T) {
	t.Parallel()
	w := notifyWidget(t)
	w.refresh(barView{DND: true})
	if w.inner.Kind != ui.KindIcon || w.inner.Icon != "do_not_disturb_on" {
		t.Fatalf("indicator = kind %v icon %q, want material DND icon", w.inner.Kind, w.inner.Icon)
	}
}

func TestNotifyWidgetActions(t *testing.T) {
	t.Parallel()
	if panelNotificationsAction != "panel:notifications" {
		t.Fatalf("left action = %q", panelNotificationsAction)
	}
	if notifyDNDAction != "notify:dnd" {
		t.Fatalf("middle action = %q", notifyDNDAction)
	}
	if notifyDNDMenuAction != "notify:dnd-menu" {
		t.Fatalf("right action = %q", notifyDNDMenuAction)
	}

	w := notifyWidget(t)
	if w.inner.Action != panelNotificationsAction {
		t.Fatalf("action = %q, want %q", w.inner.Action, panelNotificationsAction)
	}
	if w.node.Action != panelNotificationsAction {
		t.Fatalf("capsule action = %q, want %q", w.node.Action, panelNotificationsAction)
	}
}

func TestMiddleClickTogglesDND(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	bar := &Bar{}
	r.bindBarPanelActionsLocked(1, bar)

	if !bar.onAction(panelNotificationsAction, buttonMiddle) {
		t.Fatal("middle click was not handled")
	}
	if _, on := r.dndStateAt(time.Now()); !on {
		t.Fatal("middle click did not enable DND")
	}
	if !bar.onAction(panelNotificationsAction, buttonMiddle) {
		t.Fatal("second middle click was not handled")
	}
	if _, on := r.dndStateAt(time.Now()); on {
		t.Fatal("middle click did not clear DND")
	}
}

func TestRightClickRecognisesDNDMenu(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	bar := &Bar{}
	r.bindBarPanelActionsLocked(1, bar)
	if !bar.onAction(panelNotificationsAction, buttonRight) {
		t.Fatal("right click was not handled as notify:dnd-menu")
	}
}

func TestPanelNotificationsPublicName(t *testing.T) {
	t.Parallel()
	if PanelNotifications.String() != "notifications" {
		t.Fatalf("String = %q, want notifications", PanelNotifications.String())
	}
}

func notifyWidget(t *testing.T) textWidget {
	t.Helper()
	for _, w := range buildWidgets(config.Default().Bar.Right, 8, standardMetrics()) {
		if w.inner != nil && w.inner.Action == panelNotificationsAction {
			return w
		}
	}
	t.Fatal("default bar has no notifications widget")
	return textWidget{}
}
