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
	cases := map[float64]string{42: "42", 3.5: "3.5", 14.142135623730951: "14.1421356237", -0.0: "0", 1e21: "1e+21", 0.1 + 0.2: "0.3"}
	for v, want := range cases {
		if got := Format(v); got != want {
			t.Errorf("Format(%v) = %q, want %q", v, got, want)
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
