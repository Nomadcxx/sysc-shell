package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestStyleTypeSetResolvesEveryRole(t *testing.T) {
	t.Parallel()
	var set TypeSet
	for role := range set.Roles {
		set.Roles[role] = TextSpec{Family: "F", Size: 10 + role, Weight: 400}
	}
	for _, role := range []theme.TextRole{
		theme.RoleBody, theme.RoleCaption, theme.RoleLabel,
		theme.RoleTitle, theme.RoleHeadline, theme.RoleMono,
		theme.RoleDisplay, theme.RoleFigure,
	} {
		if got := set.Spec(role).Size; got != 10+int(role) {
			t.Errorf("%s size = %d, want %d", role, got, 10+int(role))
		}
	}
}

func TestStyleTypeSetFallsBackForAnUnknownRole(t *testing.T) {
	t.Parallel()
	var set TypeSet
	set.Roles[theme.RoleBody] = TextSpec{Family: "F", Size: 14, Weight: 400}
	// A role outside the table must still measure, not paint nothing.
	for _, role := range []theme.TextRole{theme.TextRole(-1), theme.TextRole(99)} {
		if got := set.Spec(role); got != set.Roles[theme.RoleBody] {
			t.Errorf("role %d = %+v, want the body spec", role, got)
		}
	}
}

func TestStyleTableCoversEveryDeclaredRole(t *testing.T) {
	t.Parallel()
	// The table is sized off the last role. If a role is added past it, this
	// is the check that says so before a frame indexes out of range.
	if textRoleCount != int(theme.RoleFigure)+1 {
		t.Fatalf("textRoleCount = %d, want %d", textRoleCount, int(theme.RoleFigure)+1)
	}
	var set TypeSet
	if len(set.Roles) != textRoleCount {
		t.Errorf("role table holds %d entries, want %d", len(set.Roles), textRoleCount)
	}
}

// TestDisplayRoleIsAddressable indexes the table at the last role. The table is
// a fixed-size array derived from that role, so if the constant were ever left
// tracking an earlier one this would be an out-of-range panic at paint -- and
// the count guard above would still have passed, because it would agree with
// the stale constant.
//
// It lives here rather than beside the role definition: internal/render imports
// internal/theme, so a test in theme naming render.TypeSet would be an import
// cycle and could never compile.
func TestDisplayRoleIsAddressable(t *testing.T) {
	t.Parallel()
	var set TypeSet
	set.Roles[theme.RoleDisplay] = TextSpec{Size: 24}
	if set.Roles[theme.RoleDisplay].Size != 24 {
		t.Error("the display role did not round-trip through the table")
	}
	if got := set.Spec(theme.RoleDisplay).Size; got != 24 {
		t.Errorf("Spec(display) = %d, want 24; the role is inside the table but Spec disagrees", got)
	}
}

func TestShapesResolveEveryRole(t *testing.T) {
	t.Parallel()
	s := Shapes{Small: 6, Medium: 12, Large: 18, Card: 12, Panel: 12}
	for _, tc := range []struct {
		shape ui.Shape
		want  int
	}{
		{ui.ShapeInherit, 99},
		{ui.ShapeSmall, 6},
		{ui.ShapeMedium, 12},
		{ui.ShapeLarge, 18},
		{ui.ShapeCard, 12},
		{ui.ShapePanel, 12},
		{ui.ShapeStadium, ShapeHalf},
		{ui.ShapeCircle, ShapeHalf},
	} {
		if got := s.For(tc.shape, 99); got != tc.want {
			t.Errorf("shape %d = %d, want %d", tc.shape, got, tc.want)
		}
	}
}

func TestStadiumSurvivesAZeroBaseRadius(t *testing.T) {
	t.Parallel()
	// Every derived role collapses to zero, but the two geometric shapes do
	// not: that is the whole reason they are roles rather than radii.
	flat := Shapes{}
	for _, shape := range []ui.Shape{ui.ShapeStadium, ui.ShapeCircle} {
		if got := flat.For(shape, 0); got != ShapeHalf {
			t.Errorf("shape %d = %d at radius zero, want the half sentinel", shape, got)
		}
	}
	if got := flat.For(ui.ShapeCard, 0); got != 0 {
		t.Errorf("card = %d at radius zero, want 0", got)
	}
}
