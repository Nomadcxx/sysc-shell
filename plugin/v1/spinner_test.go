package v1

import "testing"

func TestSpinnerValidation(t *testing.T) {
	ok := []*Node{
		{Kind: KindSpinner, Key: "launching"},
		{Kind: KindSpinner, Key: "launching", Width: 24, Tone: ToneAccent},
	}
	for _, n := range ok {
		for _, view := range []ViewKind{ViewPanel, ViewBar, ViewFloating} {
			if err := Validate(n, view); err != nil {
				t.Errorf("valid spinner %+v in %s: %v", n, view, err)
			}
		}
	}
	bad := map[string]*Node{
		"no key":      {Kind: KindSpinner},
		"too small":   {Kind: KindSpinner, Key: "s", Width: MinSpinnerSize - 1},
		"too large":   {Kind: KindSpinner, Key: "s", Width: MaxSpinnerSize + 1},
		"a value":     {Kind: KindSpinner, Key: "s", Value: 0.5},
		"with child":  {Kind: KindSpinner, Key: "s", Children: []*Node{{Kind: KindText, Text: "x"}}},
		"with events": {Kind: KindSpinner, Key: "s", ID: "s", Events: []EventKind{EventActivate}},
	}
	for name, n := range bad {
		if err := Validate(n, ViewPanel); err == nil {
			t.Errorf("%s: spinner %+v was accepted", name, n)
		}
	}
}
