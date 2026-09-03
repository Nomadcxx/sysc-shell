package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
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
	// The table is sized off RoleMono. If a role is added past it, this is
	// the check that says so before a frame indexes out of range.
	if textRoleCount != int(theme.RoleMono)+1 {
		t.Fatalf("textRoleCount = %d, want %d", textRoleCount, int(theme.RoleMono)+1)
	}
	var set TypeSet
	if len(set.Roles) != textRoleCount {
		t.Errorf("role table holds %d entries, want %d", len(set.Roles), textRoleCount)
	}
}
