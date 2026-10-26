package dep

// Scope is the entry of the coverage table: one repository, and the reason it
// is walked or left out.
//
// The set is written down rather than discovered, because a report that
// quietly covered part of the organisation would read as a report of all of
// it. A repository that is not named here is reported as uncovered, so the
// table has to be extended when the organisation grows.
type Scope struct {
	// Name is the repository name.
	Name string
	// Note says why the repository is covered.
	Note string
	// Excluded says why the repository is out of scope. It is empty for a
	// repository the report walks.
	Excluded string
}

// Coverage is the table the report walks.
//
// The three excluded repositories are the ones the 2026-10-25 governance and
// dependency handover names as out of scope, stated as critical. Everything
// else is walked, including the repositories the handover left undecided:
// they are in the pin graph, so a report that left them out would have
// holes where the drift is.
var Coverage = []Scope{
	{Name: "sysc-shell", Note: "the repository the tooling lives in"},
	{Name: "sysc-clipboard", Note: "a sibling the shell pins"},
	{Name: "sysc-terminal", Note: "a sibling the shell pins"},
	{Name: "sysc-plugins", Note: "a sibling the shell pins and a checkout the CI makes"},
	{Name: "sysc-notify", Note: "a sibling the shell pins"},
	{Name: "sysc-metrics", Note: "a sibling the shell pins"},
	{Name: "sysc-wayland", Note: "a sibling the shell pins"},
	{Name: "sysc-launch", Note: "a sibling the shell pins"},
	{Name: "sysc-tray", Note: "a sibling the shell pins"},
	{Name: "sysc-lock", Note: "a repository the CI checks out at a commit"},
	{Name: "sysc-greet", Note: "a sibling in the organisation"},
	{Name: "sysclet", Note: "a sibling in the organisation"},
	{Name: "sysc", Note: "a sibling in the organisation"},
	{Name: "sysc-compositor", Excluded: "out of scope: named as critical and excluded by the 2026-10-25 governance and dependency handover"},
	{Name: "sysc-walls", Excluded: "out of scope: named as critical and excluded by the 2026-10-25 governance and dependency handover"},
	{Name: "sysc-Go", Excluded: "out of scope: named as critical and excluded by the 2026-10-25 governance and dependency handover"},
}

// Covered reports whether the report walks a repository.
func (c Scope) Covered() bool {
	return c.Excluded == ""
}

// CoveredNames returns the names of the repositories the report walks.
func CoveredNames() []string {
	var out []string
	for _, c := range Coverage {
		if c.Covered() {
			out = append(out, c.Name)
		}
	}
	return out
}

// Known reports whether the coverage table names a repository.
func Known(name string) (Scope, bool) {
	for _, c := range Coverage {
		if c.Name == name {
			return c, true
		}
	}
	return Scope{}, false
}
