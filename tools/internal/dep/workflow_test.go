package dep

import (
	"strings"
	"testing"
)

// workflowSample is a workflow shaped like the shell's: two jobs that check
// the same two repositories out, one of them with a reason written above it.
const workflowSample = `name: CI
on:
  push:
    branches: [main]

jobs:
  test:
    steps:
      - uses: actions/checkout@v4
      # Recorder integration tests build the plugin from this pinned checkout.
      - uses: actions/checkout@v4
        with:
          repository: Nomadcxx/sysc-plugins
          ref: 0cea6f74a0c917b0edacf3832304caad021f51c7
          path: testdeps/sysc-plugins
      # The lock screen lives in its own repository; the contract tests run
      # its binary. Newer lockers follow in a grouped pin bump (sysc-1733).
      - uses: actions/checkout@v4
        with:
          repository: Nomadcxx/sysc-lock
          ref: df54457a1f6359c4379cd59aa35ad7ef12b737aa
          path: testdeps/sysc-lock
      - name: build the lock screen contract binary
        run: |
          # a comment inside a script is not a yaml comment
          cd testdeps/sysc-lock
          go build -o binary ./cmd/sysc-lock
  coverage:
    steps:
      - uses: actions/checkout@v4
        with:
          repository: Nomadcxx/sysc-plugins
          ref: 0cea6f74a0c917b0edacf3832304caad021f51c7
          path: testdeps/sysc-plugins
`

func TestParseWorkflow(t *testing.T) {
	pins := ParseWorkflow("sysc-shell", "ci.yml", workflowSample)
	if len(pins) != 3 {
		t.Fatalf("ParseWorkflow found %d pins, want 3", len(pins))
	}
	byRepo := map[string]RefPin{}
	for _, pin := range pins {
		byRepo[pin.Full()] = pin
	}
	plugins, ok := byRepo["Nomadcxx/sysc-plugins"]
	if !ok {
		t.Fatalf("the plugin checkout was not found")
	}
	if plugins.Ref != "0cea6f74a0c917b0edacf3832304caad021f51c7" {
		t.Errorf("the plugin checkout pins %q", plugins.Ref)
	}
	if plugins.Hold != "" {
		t.Errorf("the plugin checkout carries a reason it was not given: %q", plugins.Hold)
	}
	lock, ok := byRepo["Nomadcxx/sysc-lock"]
	if !ok {
		t.Fatalf("the lock checkout was not found")
	}
	if !strings.Contains(lock.Hold, "sysc-1733") {
		t.Errorf("the lock checkout carries no reason: %q", lock.Hold)
	}
	if !strings.Contains(lock.Hold, "grouped pin bump") {
		t.Errorf("the lock checkout quotes only part of its reason: %q", lock.Hold)
	}
	if lock.Line == 0 {
		t.Errorf("the lock checkout has no line number")
	}
}

func TestParseWorkflowIgnoresScriptComments(t *testing.T) {
	pins := ParseWorkflow("sysc-shell", "ci.yml", workflowSample)
	for _, pin := range pins {
		if strings.Contains(pin.Hold, "a comment inside a script") {
			t.Errorf("a comment inside a run block was read as a reason: %q", pin.Hold)
		}
	}
}

func TestParseWorkflowWithoutPins(t *testing.T) {
	pins := ParseWorkflow("sysc-shell", "vuln.yml", "name: Dependency scan\non:\n  schedule:\n    - cron: '31 5 * * 1'\n")
	if len(pins) != 0 {
		t.Fatalf("ParseWorkflow found %d pins in a workflow that has none", len(pins))
	}
}
