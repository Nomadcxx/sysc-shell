// Package workflow checks the workflow files of this repository.
//
// GitHub reads a workflow file as YAML. A file that does not parse is not
// refused by the pull request that carries it: the workflow simply never
// starts, and every push that holds the file leaves a failed run with no
// jobs behind. The check reads the files the way GitHub reads them, so a
// file that breaks is caught before it is pushed.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Check parses one workflow file and reports what is wrong with it.
func Check(name string, content []byte) error {
	var doc any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// CheckDir parses every workflow file in a directory and reports the files
// that do not parse. A directory holds the .yml and .yaml files.
func CheckDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var problems []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if err := Check(name, content); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("the workflow files do not parse:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}
