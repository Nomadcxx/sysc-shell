package shell

import "testing"

func TestParsePowerProfilesMarksTheStarredNameActive(t *testing.T) {
	t.Parallel()
	text := "" +
		"  performance:\n" +
		"    Driver:     amd_pstate\n" +
		"\n" +
		"* balanced:\n" +
		"    Driver:     amd_pstate\n" +
		"\n" +
		"  power-saver:\n" +
		"    Driver:     amd_pstate\n"
	names, active := parsePowerProfiles(text)
	want := []string{"performance", "balanced", "power-saver"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if active != "balanced" {
		t.Fatalf("active = %q, want balanced", active)
	}
}

func TestParsePowerProfilesIgnoresDetailLines(t *testing.T) {
	t.Parallel()
	names, active := parsePowerProfiles("    Driver: foo\nnot-a-profile\n")
	if len(names) != 0 || active != "" {
		t.Fatalf("names = %v active = %q, want empty", names, active)
	}
}

func TestPowerProfileLabel(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"power-saver": "Power saver",
		"balanced":    "Balanced",
		"performance": "Performance",
		"cool-custom": "cool-custom",
	}
	for name, want := range cases {
		if got := powerProfileLabel(name); got != want {
			t.Fatalf("%s: %q, want %q", name, got, want)
		}
	}
}
