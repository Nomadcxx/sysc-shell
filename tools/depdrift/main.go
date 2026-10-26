// Command depdrift reports the drift between the pins the organisation holds
// and the versions the world has published.
//
// It walks the repositories of one owner, reads the module requirements each
// one declares and the external checkouts its workflows pin, and reports for
// every pin the version it holds, the newest version that exists and the
// reason recorded beside it, when there is one.
//
// The tool reads and reports. It writes nothing, and it changes no pin: the
// bump tool carries the procedure for that.
//
// Usage:
//
//	depdrift [-owner Nomadcxx] [-fail] [-summary path] [-proxy url]
//
// -fail makes the command exit non-zero when a pin is behind and carries no
// reason. -summary names the file the markdown report is written to; the
// GitHub Actions step summary is the default when the environment names one.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Nomadcxx/sysc-shell/tools/internal/dep"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// config is what the command was asked to do.
type config struct {
	owner       string
	token       string
	proxy       string
	fail        bool
	summaryPath string
}

// run is the whole command, so a test can drive every path through it.
func run(args []string, stdout, stderr io.Writer) int {
	cfg := config{owner: "Nomadcxx"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-fail":
			cfg.fail = true
		case arg == "-owner" && i+1 < len(args):
			i++
			cfg.owner = args[i]
		case arg == "-proxy" && i+1 < len(args):
			i++
			cfg.proxy = args[i]
		case arg == "-summary" && i+1 < len(args):
			i++
			cfg.summaryPath = args[i]
		case arg == "-help" || arg == "-h":
			fmt.Fprintln(stdout, "usage: depdrift [-owner Nomadcxx] [-fail] [-summary path] [-proxy url]")
			return 0
		default:
			fmt.Fprintf(stderr, "depdrift: cannot read the argument %q\n", arg)
			return 2
		}
	}
	if cfg.token == "" {
		cfg.token = firstEnv("GITHUB_TOKEN", "GH_TOKEN")
	}
	if cfg.summaryPath == "" {
		cfg.summaryPath = os.Getenv("GITHUB_STEP_SUMMARY")
	}

	rep, err := collect(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(stderr, "depdrift: %v\n", err)
		return 2
	}
	markdown := rep.Markdown()
	fmt.Fprint(stdout, markdown)
	if cfg.summaryPath != "" {
		if err := writeSummary(cfg.summaryPath, markdown); err != nil {
			fmt.Fprintf(stderr, "depdrift: %v\n", err)
			return 2
		}
	}
	if cfg.fail && len(rep.findings()) > 0 {
		return 1
	}
	return 0
}

// firstEnv returns the value of the first environment variable that is set.
func firstEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// writeSummary writes the markdown report to the step summary file.
func writeSummary(path, markdown string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.WriteString(file, markdown)
	return err
}

// collect walks the organisation and reads every pin it holds.
func collect(ctx context.Context, cfg config) (*report, error) {
	client := dep.NewClient(cfg.token)
	repos, err := client.Repos(ctx, cfg.owner)
	if err != nil {
		return nil, err
	}
	byName := map[string]dep.Repo{}
	for _, r := range repos {
		byName[r.Name] = r
	}

	rep := &report{Owner: cfg.owner}
	for _, cov := range dep.Coverage {
		if !cov.Covered() {
			rep.Excluded = append(rep.Excluded, fmt.Sprintf("`%s` — %s", cov.Name, cov.Excluded))
			continue
		}
		repo, ok := byName[cov.Name]
		if !ok {
			rep.Missing = append(rep.Missing, cov.Name)
			continue
		}
		rr, err := walk(ctx, client, cfg, repo, byName)
		if err != nil {
			return nil, err
		}
		rep.Repos = append(rep.Repos, *rr)
	}
	for _, r := range repos {
		if !strings.HasPrefix(strings.ToLower(r.Name), "sysc") {
			continue
		}
		if _, known := dep.Known(r.Name); !known {
			rep.Uncovered = append(rep.Uncovered, r.Name)
		}
	}
	sort.Strings(rep.Uncovered)
	sort.Strings(rep.Missing)
	return rep, nil
}

// walk reads one repository: its module requirements and its workflow pins.
func walk(ctx context.Context, client *dep.Client, cfg config, repo dep.Repo, org map[string]dep.Repo) (*repoReport, error) {
	rr := &repoReport{Name: repo.Name, Branch: repo.DefaultBranch}
	if repo.Private {
		rr.Notes = append(rr.Notes, "The repository is private.")
	}

	goMod, err := client.File(ctx, cfg.owner, repo.Name, "go.mod", repo.DefaultBranch)
	if err != nil {
		if !isNotFound(err) {
			return nil, fmt.Errorf("%s: %w", repo.Name, err)
		}
		rr.Notes = append(rr.Notes, "The repository has no go.mod.")
	} else {
		content := string(goMod)
		if line, ok := dep.GoDirectiveLine(content); ok {
			rr.Toolchain = line
			comment := dep.GoDirectiveComment(content)
			if len(comment) > 0 {
				rr.ToolchainNote = strings.TrimSpace(strings.ReplaceAll(strings.Join(comment, " "), "//", ""))
			}
		}
		versions := map[string][]string{}
		for _, pin := range dep.ParseGoMod(content) {
			row := moduleRow{
				Module:   pin.Module,
				Pinned:   pin.Version,
				Hold:     pin.Hold,
				Indirect: pin.Indirect,
			}
			published, ok := versions[pin.Module]
			if !ok {
				published, err = dep.Versions(ctx, client.HTTP, cfg.proxy, pin.Module)
				if err != nil {
					if !isNotPublished(err) {
						return nil, fmt.Errorf("%s: %w", pin.Module, err)
					}
					published = nil
				}
				versions[pin.Module] = published
			}
			fillModule(&row, published)
			rr.Modules = append(rr.Modules, row)
		}
		sort.Slice(rr.Modules, func(i, j int) bool {
			return rr.Modules[i].Module < rr.Modules[j].Module
		})
	}

	entries, err := client.ListDir(ctx, cfg.owner, repo.Name, ".github/workflows", repo.DefaultBranch)
	if err != nil {
		if !isNotFound(err) {
			return nil, fmt.Errorf("%s: %w", repo.Name, err)
		}
	} else {
		for _, entry := range entries {
			if entry.Type != "file" || !isWorkflow(entry.Name) {
				continue
			}
			data, err := client.File(ctx, cfg.owner, repo.Name, entry.Path, repo.DefaultBranch)
			if err != nil {
				if isNotFound(err) {
					continue
				}
				return nil, fmt.Errorf("%s: %w", entry.Path, err)
			}
			for _, pin := range dep.ParseWorkflow(repo.Name, entry.Name, string(data)) {
				row := refRow{Repository: pin.Full(), Pinned: pin.Ref, Hold: pin.Hold}
				head, behind, err := headBehind(ctx, client, cfg, pin, org)
				switch {
				case err != nil:
					row.Note = "the pin cannot be compared: " + err.Error()
				default:
					row.Head = head
					row.Behind = behind
				}
				rr.Refs = append(rr.Refs, row)
			}
		}
		rr.Refs = dedupeRefs(rr.Refs)
		sort.Slice(rr.Refs, func(i, j int) bool {
			return rr.Refs[i].Repository < rr.Refs[j].Repository
		})
	}
	return rr, nil
}

// headBehind measures a workflow pin against the branch of the repository it
// checks out.
func headBehind(ctx context.Context, client *dep.Client, cfg config, pin dep.RefPin, org map[string]dep.Repo) (string, int, error) {
	branch := "main"
	if repo, ok := org[pin.Name]; ok && repo.DefaultBranch != "" {
		branch = repo.DefaultBranch
	} else {
		repos, err := client.Repos(ctx, cfg.owner)
		if err != nil {
			return "", 0, err
		}
		for _, r := range repos {
			if r.Name == pin.Name {
				branch = r.DefaultBranch
			}
		}
	}
	head, err := client.HeadSHA(ctx, pin.Owner, pin.Name, branch)
	if err != nil {
		return "", 0, err
	}
	behind, err := client.CommitsAhead(ctx, pin.Owner, pin.Name, pin.Ref, head)
	if err != nil {
		return head, 0, err
	}
	return head, behind, nil
}

// dedupeRefs merges the workflow pins that name the same commit.
//
// One pin is often written in more than one job of the same workflow: the
// test job and the coverage job check out the same tree. They are one pin,
// and a reason written beside one of them is a reason for all of them.
func dedupeRefs(refs []refRow) []refRow {
	type key struct {
		repository string
		pinned     string
	}
	order := []key{}
	first := map[key]int{}
	for i, ref := range refs {
		k := key{repository: ref.Repository, pinned: ref.Pinned}
		at, seen := first[k]
		if !seen {
			first[k] = i
			order = append(order, k)
			continue
		}
		if refs[at].Hold == "" {
			refs[at].Hold = ref.Hold
		}
		if refs[at].Note == "" {
			refs[at].Note = ref.Note
		}
		if refs[at].Head == "" {
			refs[at].Head = ref.Head
		}
		if refs[at].Behind == 0 {
			refs[at].Behind = ref.Behind
		}
	}
	out := make([]refRow, 0, len(order))
	for _, k := range order {
		out = append(out, refs[first[k]])
	}
	return out
}

// fillModule fills the drift columns of a module row.
func fillModule(row *moduleRow, published []string) {
	if dep.IsPseudoVersion(row.Pinned) {
		// A pseudo-version names a commit, not a release. There is nothing
		// to count it behind; the newest release is named beside it so the
		// reader can see what the pin sits against.
		row.Behind = -1
		row.Note = "no release target; cut a tag first"
		if len(published) > 0 {
			row.Latest = dep.LatestRelease(published)
			return
		}
		row.Latest = "—"
		return
	}
	if len(published) == 0 {
		row.Latest = "—"
		row.Note = "no published version"
		row.Behind = -1
		return
	}
	row.Latest = dep.LatestRelease(published)
	row.Behind = dep.ReleasesBehind(row.Pinned, published)
}

// isWorkflow reports whether a file name is a workflow file.
func isWorkflow(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

// isNotFound reports whether an API error is a missing path.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

// isNotPublished reports whether an error is a module the proxy does not
// carry.
func isNotPublished(err error) bool {
	return err != nil && strings.Contains(err.Error(), dep.ErrNotPublished.Error())
}
