package main

import "testing"

func TestParseOptions(t *testing.T) {
	t.Parallel()

	got, err := parseOptions([]string{"--output", "DP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "DP-1" {
		t.Fatalf("output = %q, want DP-1", got.Output)
	}
}

func TestParseOptionsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--bogus"}); err == nil {
		t.Fatal("parseOptions accepted an unknown flag")
	}
}

func TestParseOptionsRejectsMissingOutputValue(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--output"}); err == nil {
		t.Fatal("parseOptions accepted --output without a value")
	}
}
