package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
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

func TestANotifyWidgetRendersTheBellRune(t *testing.T) {
	t.Parallel()
	w := notifyWidget(t)
	w.refresh(barView{})
	want, ok := render.IconByName("notifications")
	if !ok {
		t.Fatal("notifications glyph missing from the catalogue")
	}
	if got := widgetText(w.inner); got != string(want) {
		t.Fatalf("text = %q, want the notifications rune", got)
	}
}

func TestUnreadPaintsASixPixelErrorChild(t *testing.T) {
	t.Parallel()
	w := notifyWidget(t)
	w.refresh(barView{Unread: 3})
	dot := findErrorDot(w.inner)
	if dot == nil {
		t.Fatal("unread > 0 painted no 6 px Error child")
	}
	if dot.Width != 6 {
		t.Fatalf("dot width = %d, want 6", dot.Width)
	}
	if dot.Fill != ui.FillError {
		t.Fatalf("dot fill = %v, want FillError", dot.Fill)
	}

	w.refresh(barView{})
	if findErrorDot(w.inner) != nil {
		t.Fatal("unread 0 kept the Error child")
	}
	want, _ := render.IconByName("notifications")
	if got := widgetText(w.inner); got != string(want) {
		t.Fatal("unread 0 hid the bell")
	}
}

func TestDNDSwapsToNotificationsOff(t *testing.T) {
	t.Parallel()
	w := notifyWidget(t)
	w.refresh(barView{DND: true})
	want, ok := render.IconByName("notifications-off")
	if !ok {
		t.Fatal("notifications-off glyph missing from the catalogue")
	}
	if got := widgetText(w.inner); got != string(want) {
		t.Fatalf("text = %q, want the notifications-off rune", got)
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
	for _, w := range buildWidgets(config.Default().Bar.Right, 8) {
		if w.inner != nil && w.inner.Action == panelNotificationsAction {
			return w
		}
	}
	t.Fatal("default bar has no notifications widget")
	return textWidget{}
}

func widgetText(n *ui.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == ui.KindText && n.Text != "" {
		return n.Text
	}
	for _, c := range n.Children {
		if s := widgetText(c); s != "" {
			return s
		}
	}
	return ""
}

func findErrorDot(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindCapsule && n.Fill == ui.FillError && n.Width == 6 {
		return n
	}
	for _, c := range n.Children {
		if d := findErrorDot(c); d != nil {
			return d
		}
	}
	return nil
}
