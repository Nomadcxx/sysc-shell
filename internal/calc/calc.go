// Package calc evaluates the launcher's arithmetic: a recursive-descent
// parser over float64 with a fixed grammar and no variables.
package calc

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
)

var ErrSyntax = errors.New("calc: invalid expression")

type parser struct {
	s        string
	pos      int
	operator bool // saw a binary operator or a function call
}

func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("×", "*", "÷", "/").Replace(s)
	return strings.Join(strings.Fields(s), "")
}

func parse(expr string) (float64, bool, error) {
	p := &parser{s: normalise(expr)}
	if p.s == "" {
		return 0, false, ErrSyntax
	}
	v, err := p.sum()
	if err != nil {
		return 0, false, err
	}
	if p.pos != len(p.s) {
		return 0, false, ErrSyntax
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false, ErrSyntax
	}
	return v, p.operator, nil
}

func Eval(expr string) (float64, error) { v, _, err := parse(expr); return v, err }

func IsExpression(s string) bool { _, op, err := parse(s); return err == nil && op }

func Format(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'g', 12, 64)
}

func (p *parser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

// sum := product (('+'|'-') product)*
func (p *parser) sum() (float64, error) {
	v, err := p.product()
	for err == nil && (p.peek() == '+' || p.peek() == '-') {
		op := p.peek()
		p.pos++
		p.operator = true
		var r float64
		if r, err = p.product(); err == nil {
			if op == '+' {
				v += r
			} else {
				v -= r
			}
		}
	}
	return v, err
}

// product := unary (('*'|'/'|'%') unary)*
func (p *parser) product() (float64, error) {
	v, err := p.unary()
	for err == nil && (p.peek() == '*' || p.peek() == '/' || p.peek() == '%') {
		op := p.peek()
		p.pos++
		if p.peek() == '*' {
			return 0, ErrSyntax // "**" is not an operator here
		}
		p.operator = true
		var r float64
		if r, err = p.unary(); err == nil {
			switch op {
			case '*':
				v *= r
			case '/':
				v /= r
			case '%':
				v = math.Mod(v, r)
			}
		}
	}
	return v, err
}

// unary := '-' unary | power   (so -2^2 is -(2^2))
func (p *parser) unary() (float64, error) {
	if p.peek() == '-' {
		p.pos++
		v, err := p.unary()
		return -v, err
	}
	return p.power()
}

// power := atom ('^' unary)?   right-associative
func (p *parser) power() (float64, error) {
	v, err := p.atom()
	if err == nil && p.peek() == '^' {
		p.pos++
		p.operator = true
		var r float64
		if r, err = p.unary(); err == nil {
			v = math.Pow(v, r)
		}
	}
	return v, err
}

var functions = map[string]func(float64) float64{
	"sqrt": math.Sqrt, "abs": math.Abs, "ln": math.Log, "log": math.Log10,
	"sin": math.Sin, "cos": math.Cos, "tan": math.Tan,
	"floor": math.Floor, "ceil": math.Ceil, "round": math.Round,
}

// atom := number | constant | function '(' sum ')' | '(' sum ')'
func (p *parser) atom() (float64, error) {
	switch c := p.peek(); {
	case c == '(':
		p.pos++
		v, err := p.sum()
		if err != nil || p.peek() != ')' {
			return 0, ErrSyntax
		}
		p.pos++
		return v, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.s) && (p.s[p.pos] >= '0' && p.s[p.pos] <= '9' || p.s[p.pos] == '.') {
			p.pos++
		}
		if p.pos < len(p.s) && p.s[p.pos] == 'e' && p.pos+1 < len(p.s) &&
			(unicode.IsDigit(rune(p.s[p.pos+1])) || p.s[p.pos+1] == '-' || p.s[p.pos+1] == '+') {
			p.pos += 2
			for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
				p.pos++
			}
		}
		v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
		if err != nil {
			return 0, ErrSyntax
		}
		if p.peek() == '(' {
			return 0, ErrSyntax // no implicit multiplication
		}
		return v, nil
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.s) && p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z' {
			p.pos++
		}
		name := p.s[start:p.pos]
		if fn, ok := functions[name]; ok && p.peek() == '(' {
			p.pos++
			p.operator = true
			v, err := p.sum()
			if err != nil || p.peek() != ')' {
				return 0, ErrSyntax
			}
			p.pos++
			return fn(v), nil
		}
		switch name {
		case "pi":
			return math.Pi, nil
		case "e":
			return math.E, nil
		}
	}
	return 0, ErrSyntax
}
