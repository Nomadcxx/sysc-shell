package calc

import (
	"math"
	"testing"
)

func TestEval(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"6*7", 42}, {"1+2*3", 7}, {"(1+2)*3", 9}, {"2^3^2", 512}, {"-2^2", -4}, {"10%4", 2},
		{"7/2", 3.5}, {"1e3+1", 1001}, {"sqrt(16)", 4}, {"abs(-3)", 3}, {"ln(e)", 1}, {"log(1000)", 3},
		{"sin(pi/2)", 1}, {"cos(0)", 1}, {"tan(0)", 0}, {"floor(2.7)", 2}, {"ceil(2.1)", 3}, {"round(2.5)", 3},
		{"6×7", 42}, {"84÷2", 42}, {"  SQRT( 16 ) ", 4}, {"2(3)", math.NaN()},
	}
	for _, tc := range cases {
		got, err := Eval(tc.in)
		if math.IsNaN(tc.want) {
			if err == nil {
				t.Errorf("%q: want an error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%q = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	for _, in := range []string{"", "2+", "(1", "1)", "foo(2)", "1/0", "sqrt(-1)", "2**3", "1,5"} {
		if _, err := Eval(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := map[float64]string{42: "42", 3.5: "3.5", 14.142135623730951: "14.1421356237", 1e21: "1e+21", 0.1 + 0.2: "0.3"}
	for v, want := range cases {
		if got := Format(v); got != want {
			t.Errorf("Format(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestFormatNegativeZero(t *testing.T) {
	v := math.Copysign(0, -1)
	if !math.Signbit(v) {
		t.Fatal("test input is not negative zero")
	}
	if got := Format(v); got != "0" {
		t.Fatalf("Format(math.Copysign(0, -1)) = %q, want %q", got, "0")
	}
}

func TestConvertLength(t *testing.T) {
	cases := []struct {
		in       string
		want     float64
		wantUnit string
		wantOK   bool
	}{
		{"12 km to mi", 7.456454306848, "mi", true},
		{"2 + 1 km to m", 3000, "m", true},
		{"1 mi to km", 1.609344, "km", true},
		{"1e308 km to mi", 6.21371192237334e307, "mi", true},
		{"12 kg to mi", 0, "", false},
		{"12 km to kg", 0, "", false},
		{"12 km to", 0, "", false},
		{"1e308 km to mm", 0, "", false},
	}
	for _, tc := range cases {
		got, unit, ok := ConvertLength(tc.in)
		tolerance := math.Max(1, math.Abs(tc.want)) * 1e-12
		if ok != tc.wantOK || unit != tc.wantUnit || ok && math.Abs(got-tc.want) > tolerance {
			t.Errorf("ConvertLength(%q) = (%v, %q, %v), want (%v, %q, %v)",
				tc.in, got, unit, ok, tc.want, tc.wantUnit, tc.wantOK)
		}
	}
}

// Review focus 1.
func TestInlineRuleNegatives(t *testing.T) {
	for _, s := range []string{"2048", "e", "pi", "firefox", "-3", "(2)", "", "6*"} {
		if IsExpression(s) {
			t.Errorf("%q counted as an expression", s)
		}
	}
	for _, s := range []string{"6*7", "sqrt(2)", "2^10", "1-1", "abs(3)"} {
		if !IsExpression(s) {
			t.Errorf("%q not counted as an expression", s)
		}
	}
}
