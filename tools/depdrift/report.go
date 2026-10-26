package main

import (
	"fmt"
	"sort"
	"strings"
)

// moduleRow is one module requirement of one repository, with the drift it
// carries.
type moduleRow struct {
	// Module is the module path.
	Module string
	// Pinned is the version the repository requires.
	Pinned string
	// Latest is the newest published version of the module.
	Latest string
	// Behind is the number of published releases newer than the pin. It is
	// -1 when the pin has no release target: a pseudo-version names a
	// commit, not a release.
	Behind int
	// Hold is the reason the pin is deliberately held, empty when there is
	// none.
	Hold string
	// Note carries what the row says that the columns do not.
	Note string
	// Indirect is true when the requirement is marked indirect.
	Indirect bool
}

// refRow is one external checkout a workflow pins.
type refRow struct {
	// Repository is the repository being checked out.
	Repository string
	// Pinned is the commit the workflow pins.
	Pinned string
	// Head is the commit the default branch points at today.
	Head string
	// Behind is the number of commits the pin is behind the branch by.
	Behind int
	// Hold is the reason the pin is deliberately held, empty when there is
	// none.
	Hold string
	// Note carries what the row says that the columns do not.
	Note string
}

// repoReport is what the report says about one repository.
type repoReport struct {
	// Name is the repository name.
	Name string
	// Branch is the default branch the report read.
	Branch string
	// Toolchain is the go directive the repository declares.
	Toolchain string
	// ToolchainNote carries the reason the floor sits where it does.
	ToolchainNote string
	// Modules are the module requirements of the repository.
	Modules []moduleRow
	// Refs are the external checkouts the workflows pin.
	Refs []refRow
	// Notes are the facts about the repository the tables do not carry.
	Notes []string
}

// report is the whole drift report.
type report struct {
	// Owner is the account the repositories belong to.
	Owner string
	// Repos is the report per repository.
	Repos []repoReport
	// Uncovered names the repositories the organisation holds that the
	// coverage table does not name.
	Uncovered []string
	// Missing names the repositories the coverage table names that the
	// organisation does not hold.
	Missing []string
	// Excluded names the repositories the table leaves out, with the
	// reason.
	Excluded []string
}

// finding is one pin that is behind and carries no reason for being there.
type finding struct {
	// Repo is the repository the pin lives in.
	Repo string
	// Pin is the module path or the repository name.
	Pin string
	// Pinned is the version or commit the pin holds.
	Pinned string
	// Latest is the version the pin could move to.
	Latest string
	// Drift says how far the pin is behind.
	Drift string
}

// findings returns the pins that are behind with no reason recorded.
//
// A pin with a reason is a decision. A pin without one is a finding: nothing
// in the tree says why it sits where it does, so nothing says when it should
// move.
func (r *report) findings() []finding {
	var out []finding
	for _, repo := range r.Repos {
		for _, m := range repo.Modules {
			if m.Behind >= 2 && m.Hold == "" {
				out = append(out, finding{
					Repo:   repo.Name,
					Pin:    m.Module,
					Pinned: m.Pinned,
					Latest: m.Latest,
					Drift:  releasesText(m.Behind),
				})
			}
		}
		for _, ref := range repo.Refs {
			if ref.Behind >= 2 && ref.Hold == "" {
				out = append(out, finding{
					Repo:   repo.Name,
					Pin:    ref.Repository,
					Pinned: ref.Pinned,
					Latest: ref.Head,
					Drift:  commitsText(ref.Behind),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Pin < out[j].Pin
	})
	return out
}

// Markdown renders the report.
func (r *report) Markdown() string {
	var b strings.Builder
	findings := r.findings()

	b.WriteString("# Dependency drift\n\n")
	fmt.Fprintf(&b, "Owner `%s`. Repositories walked: %d. Findings: %d.\n\n",
		r.Owner, len(r.Repos), len(findings))

	if len(r.Excluded) > 0 {
		b.WriteString("Excluded from the walk:\n\n")
		for _, e := range r.Excluded {
			fmt.Fprintf(&b, "- %s\n", e)
		}
		b.WriteString("\n")
	}
	if len(r.Uncovered) > 0 {
		b.WriteString("Repositories the organisation holds that the coverage table does not name:\n\n")
		for _, u := range r.Uncovered {
			fmt.Fprintf(&b, "- `%s`\n", u)
		}
		b.WriteString("\n")
	}
	if len(r.Missing) > 0 {
		b.WriteString("Repositories the coverage table names that the organisation does not hold:\n\n")
		for _, m := range r.Missing {
			fmt.Fprintf(&b, "- `%s`\n", m)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Findings\n\n")
	if len(findings) == 0 {
		b.WriteString("None. Every pin that is behind carries a reason.\n\n")
	} else {
		b.WriteString("| Repository | Pin | Pinned | Current | Drift |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | %s |\n",
				f.Repo, f.Pin, f.Pinned, f.Latest, f.Drift)
		}
		b.WriteString("\n")
	}

	for _, repo := range r.Repos {
		fmt.Fprintf(&b, "## %s\n\n", repo.Name)
		if repo.Branch != "" {
			fmt.Fprintf(&b, "Default branch `%s`.", repo.Branch)
			if repo.Toolchain != "" {
				fmt.Fprintf(&b, " Toolchain floor `%s`%s.", repo.Toolchain, noteSuffix(repo.ToolchainNote))
			}
			b.WriteString("\n\n")
		}
		for _, n := range repo.Notes {
			fmt.Fprintf(&b, "%s\n\n", n)
		}
		if len(repo.Modules) > 0 {
			b.WriteString("| Module | Pinned | Latest | Drift | Hold |\n")
			b.WriteString("|---|---|---|---|---|\n")
			for _, m := range repo.Modules {
				fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s | %s |\n",
					m.Module, m.Pinned, m.Latest, driftText(m), holdText(m.Hold))
			}
			b.WriteString("\n")
		}
		if len(repo.Refs) > 0 {
			b.WriteString("| Checkout | Pinned | Head | Drift | Hold |\n")
			b.WriteString("|---|---|---|---|---|\n")
			for _, ref := range repo.Refs {
				drift := "current"
				if ref.Note != "" {
					drift = ref.Note
				} else if ref.Behind > 0 {
					drift = commitsText(ref.Behind)
				}
				fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s | %s |\n",
					ref.Repository, short(ref.Pinned), short(ref.Head), drift, holdText(ref.Hold))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// driftText says how far a module pin is behind.
func driftText(m moduleRow) string {
	switch {
	case m.Note != "":
		return m.Note
	case m.Behind > 0:
		return releasesText(m.Behind)
	default:
		return "current"
	}
}

// releasesText says how many releases a pin is behind.
func releasesText(n int) string {
	if n == 1 {
		return "1 release behind"
	}
	return fmt.Sprintf("%d releases behind", n)
}

// commitsText says how many commits a pin is behind.
func commitsText(n int) string {
	if n == 1 {
		return "1 commit behind"
	}
	return fmt.Sprintf("%d commits behind", n)
}

// holdText says whether a pin carries a reason.
func holdText(hold string) string {
	if hold == "" {
		return "—"
	}
	return hold
}

// noteSuffix appends a reason to a sentence.
func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
}

// short writes a commit hash short.
func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
