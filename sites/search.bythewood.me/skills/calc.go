package skills

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A question like "30*27" should not spend fifteen seconds fetching web pages
// to be told what a calculator knows. This is a small recursive descent parser
// over the arithmetic a person types into a search box.
//
// It is deliberately narrow. Anything it does not fully understand it declines,
// and the question goes to the web instead, because a calculator that guesses
// is worse than no calculator.

// Calculation is an arithmetic question answered without leaving the process.
type Calculation struct {
	Expression string
	Result     float64
	Pretty     string
}

// TryCalculate answers a question if, and only if, the whole of it is
// arithmetic. The check is strict: "what is 5% of 20" parses, "how much is a
// 5% mortgage on 200k" does not, and should not.
func TryCalculate(question string) (*Calculation, bool) {
	expr := normalizeExpression(question)
	if expr == "" {
		return nil, false
	}
	p := &parser{src: []rune(expr)}
	v, err := p.parseExpr()
	if err != nil {
		return nil, false
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return nil, false
	}
	// A bare number is not a question, and an overflow is not an answer.
	if !p.sawOperator || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, false
	}
	return &Calculation{Expression: expr, Result: v, Pretty: formatNumber(v)}, true
}

// normalizeExpression strips the words a person wraps an expression in and
// rewrites the symbols they type for the ones the parser reads. It returns ""
// when anything is left that is not arithmetic, which is what keeps a real
// question from being answered by the calculator.
func normalizeExpression(q string) string {
	s := strings.ToLower(strings.TrimSpace(q))
	s = strings.TrimSuffix(s, "?")
	s = strings.TrimSuffix(s, "=")

	for _, prefix := range []string{
		"what is", "whats", "what's", "calculate", "compute", "how much is",
		"how many is", "solve", "evaluate", "work out", "what does",
	} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "equal"))
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "equals"))

	// Words people type for operators.
	for from, to := range map[string]string{
		" plus ": "+", " minus ": "-", " times ": "*",
		" multiplied by ": "*", " divided by ": "/", " over ": "/",
		" to the power of ": "^", " mod ": "%", " modulo ": "%",
		"×": "*", "÷": "/", "−": "-", "•": "*",
	} {
		s = strings.ReplaceAll(s, from, to)
	}
	// "5% of 20" is the one percent form worth supporting, and it has to be
	// rewritten before % becomes a modulo operator.
	s = strings.ReplaceAll(s, "% of ", "%*")
	s = strings.ReplaceAll(s, " of ", "*")

	// Thousands separators, but only between digits, so 1,234 works and a list
	// does not silently become a number.
	var b strings.Builder
	for i, r := range s {
		if r == ',' && i > 0 && i+1 < len(s) &&
			unicode.IsDigit(rune(s[i-1])) && unicode.IsDigit(rune(s[i+1])) {
			continue
		}
		b.WriteRune(r)
	}
	s = b.String()

	// Anything left that is not arithmetic disqualifies the whole question.
	for _, r := range s {
		if unicode.IsDigit(r) || unicode.IsSpace(r) {
			continue
		}
		if strings.ContainsRune("+-*/^%().", r) {
			continue
		}
		return ""
	}
	return strings.TrimSpace(s)
}

type parser struct {
	src         []rune
	pos         int
	sawOperator bool
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && unicode.IsSpace(p.src[p.pos]) {
		p.pos++
	}
}

func (p *parser) peek() rune {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// parseExpr handles + and -, the loosest binding.
func (p *parser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '+':
			p.pos++
			p.sawOperator = true
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v += r
		case '-':
			p.pos++
			p.sawOperator = true
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v -= r
		default:
			return v, nil
		}
	}
}

// parseTerm handles *, / and %.
func (p *parser) parseTerm() (float64, error) {
	v, err := p.parsePower()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '*':
			p.pos++
			p.sawOperator = true
			r, err := p.parsePower()
			if err != nil {
				return 0, err
			}
			v *= r
		case '/':
			p.pos++
			p.sawOperator = true
			r, err := p.parsePower()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("divide by zero")
			}
			v /= r
		case '%':
			p.pos++
			p.sawOperator = true
			r, err := p.parsePower()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("modulo zero")
			}
			v = math.Mod(v, r)
		default:
			return v, nil
		}
	}
}

// parsePower handles ^, which binds tightest and is right associative.
func (p *parser) parsePower() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	if p.peek() == '^' {
		p.pos++
		p.sawOperator = true
		exp, err := p.parsePower()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *parser) parseUnary() (float64, error) {
	switch p.peek() {
	case '-':
		p.pos++
		v, err := p.parseUnary()
		return -v, err
	case '+':
		p.pos++
		return p.parseUnary()
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (float64, error) {
	switch p.peek() {
	case 0:
		return 0, fmt.Errorf("expression ends early")
	case '(':
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, fmt.Errorf("unclosed bracket")
		}
		p.pos++
		return v, nil
	}

	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) && (unicode.IsDigit(p.src[p.pos]) || p.src[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected a number")
	}
	// A trailing % means percent, so 5%*20 reads as five percent of twenty.
	v, err := strconv.ParseFloat(string(p.src[start:p.pos]), 64)
	if err != nil {
		return 0, err
	}
	if p.pos < len(p.src) && p.src[p.pos] == '%' && !p.isModulo() {
		p.pos++
		v /= 100
	}
	return v, nil
}

// isModulo tells "5 % 3" from "5%*20". Percent is only a unit when an operator
// or the end of the expression follows it.
func (p *parser) isModulo() bool {
	i := p.pos + 1
	for i < len(p.src) && unicode.IsSpace(p.src[i]) {
		i++
	}
	if i >= len(p.src) {
		return true
	}
	return unicode.IsDigit(p.src[i]) || p.src[i] == '(' || p.src[i] == '.'
}

// formatNumber prints a result the way a person would write it: no trailing
// zeroes, thousands separated, and never in scientific notation for anything of
// a size a person typed.
func formatNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return group(strconv.FormatFloat(v, 'f', 0, 64))
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if len(s) > 18 {
		s = strconv.FormatFloat(v, 'f', 10, 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return group(s[:i]) + s[i:]
	}
	return group(s)
}

func group(intPart string) string {
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	if len(intPart) <= 3 {
		if neg {
			return "-" + intPart
		}
		return intPart
	}
	var out []byte
	for i, c := range []byte(intPart) {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// Calculator is the skill wrapper. The parser above is the whole of it, so
// this never touches the network and never fails slowly.
type Calculator struct{}

func (Calculator) Card() Card {
	return Card{
		Name: "maths",
		Does: "evaluates an arithmetic expression the user has written out, and returns the number.",
		Fires: []string{
			"30 * 27",
			"what is 15% of 240",
			"(1200 + 450) / 3",
			"2^10",
			"how much is 45.99 times 3",
		},
		NotFor: []string{
			"how does compound interest work",
			"what is the average house price in london",
			"how many calories in a banana",
			"convert 30 celsius to fahrenheit",
			"what is the square root of the population of france",
		},
		Keywords: nil, // the parser is the matcher, and it is exact
	}
}

func (Calculator) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()
	calc, ok := TryCalculate(question)
	if !ok {
		return nil, nil
	}
	return &Result{
		Skill:   "maths",
		Shape:   "factual",
		Text:    fmt.Sprintf("**%s**\n\n`%s = %s`", calc.Pretty, calc.Expression, calc.Pretty),
		Elapsed: d.now().Sub(start).Round(time.Millisecond).String(),
	}, nil
}
