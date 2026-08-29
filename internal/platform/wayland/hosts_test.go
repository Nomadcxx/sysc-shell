package wayland

import "testing"

func TestHostSetCreatesOneHostPerGlobal(t *testing.T) {
	t.Parallel()
	s := newHostSet()

	first, created := s.add(3, nil)
	if !created || first == nil {
		t.Fatalf("add(3) = %v, %v; want a new host", first, created)
	}
	again, created := s.add(3, nil)
	if created {
		t.Fatal("add(3) twice created a second host")
	}
	if again != first {
		t.Fatal("add(3) twice returned a different host")
	}
	if s.len() != 1 {
		t.Fatalf("len() = %d, want 1", s.len())
	}
}

func TestHostSetReconnectUnderNewGlobalIsNotADuplicate(t *testing.T) {
	t.Parallel()
	s := newHostSet()

	old, _ := s.add(3, nil)
	old.connector = "DP-1"
	old.doneSeen = true

	// The monitor disappears and returns under a different global name.
	if _, ok := s.remove(3); !ok {
		t.Fatal("remove(3) did not report the host")
	}
	fresh, created := s.add(9, nil)
	if !created {
		t.Fatal("add(9) did not create a host")
	}
	fresh.connector = "DP-1"
	fresh.doneSeen = true

	if s.len() != 1 {
		t.Fatalf("len() = %d, want 1 after reconnect", s.len())
	}
	got, ok := s.byConnector("DP-1")
	if !ok || got != fresh {
		t.Fatal("byConnector(DP-1) did not resolve to the new host")
	}
	if _, ok := s.get(3); ok {
		t.Fatal("the removed global is still present")
	}
}

func TestHostSetEachIsArrivalOrdered(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	s.add(5, nil)
	s.add(2, nil)
	s.add(8, nil)
	s.remove(2)
	s.add(2, nil)

	var order []uint32
	for _, h := range s.each() {
		order = append(order, h.global)
	}
	want := []uint32{5, 8, 2}
	if len(order) != len(want) {
		t.Fatalf("each() = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("each() = %v, want %v", order, want)
		}
	}
}
