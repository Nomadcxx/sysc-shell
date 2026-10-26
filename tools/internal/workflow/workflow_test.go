package workflow

import (
	"os"
	"path/filepath"
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
