package shell

import "testing"

func TestMenuOpensOnActivateAndSelectsOnEnter(t *testing.T) {
	t.Parallel()
	m := NewMenu([]string{"dark", "light"}, 0)
	m.Open()
	if !m.Opened() {
		t.Fatal("Open must mark the menu open")
	}
	m.Next()
	m.Next() // wraps to 0
	if got := m.Select(); got != 0 {
		t.Fatalf("wrap selection = %d, want 0", got)
	}
	if m.Opened() {
		t.Fatal("Select must close")
	}
}

func TestMenuEscapeReturnsToField(t *testing.T) {
	t.Parallel()
	m := NewMenu([]string{"dark", "light"}, 0)
	m.Open()
	m.Next()
	m.Cancel()
	if m.Opened() {
		t.Fatal("Escape while open must close")
	}
	if m.Index() != 0 {
		t.Fatalf("Escape must keep the committed value, got %d", m.Index())
	}
}
