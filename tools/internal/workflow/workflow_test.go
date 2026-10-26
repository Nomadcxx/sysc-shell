package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the package directory to the directory that holds
// go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// The workflow files of this repository are read by GitHub as YAML. A file
// that does not parse never runs, so the suite reads them all.
func TestTheWorkflowFilesOfThisRepositoryParse(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	if err := CheckDir(dir); err != nil {
		t.Fatal(err)
	}
}

// A run script that puts free text at the first column ends the block
// scalar, and the text after it is read as YAML. GitHub refuses the file
// and the workflow never starts.
func TestARunScriptThatDedentsIsRefused(t *testing.T) {
	broken := "name: test\n" +
		"on: push\n" +
		"jobs:\n" +
		"  b:\n" +
		"    runs-on: ubuntu-latest\n" +
		"    steps:\n" +
		"      - run: |\n" +
		"          echo one\n" +
		"The free text of a body argument\n" +
		"          echo two\n"
	if err := Check("broken.yml", []byte(broken)); err == nil {
		t.Fatal("the file with free text at the first column was accepted")
	}
}

// A workflow file that reads as YAML is accepted.
func TestAWorkflowFileThatParsesIsAccepted(t *testing.T) {
	good := "name: test\n" +
		"on: push\n" +
		"jobs:\n" +
		"  b:\n" +
		"    runs-on: ubuntu-latest\n" +
		"    steps:\n" +
		"      - run: |\n" +
		"          echo one\n" +
		"          echo two\n"
	if err := Check("good.yml", []byte(good)); err != nil {
		t.Fatal(err)
	}
}

// The check reads the workflow files of a directory and leaves the other
// files of the directory alone.
func TestTheCheckReadsTheWorkflowFilesOfADirectory(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("good.yml", "name: test\non: push\n")
	write("broken.yml", "name: test\nfree text\n")
	write("notes.txt", "not a workflow\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := CheckDir(dir)
	if err == nil {
		t.Fatal("the directory with a broken workflow file was accepted")
	}
	if !strings.Contains(err.Error(), "broken.yml") {
		t.Fatalf("the report does not name the broken file: %v", err)
	}
	if strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("the report names a file that is not a workflow: %v", err)
	}
}

// A directory that cannot be read is a problem the check reports.
func TestTheCheckReportsADirectoryItCannotRead(t *testing.T) {
	if err := CheckDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("the missing directory was accepted")
	}
}
