package tools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"strconv"
	"strings"
)

// The functions calc understands. A mortgage payment needs a power, and without
// one the model writes the amortization formula with `^`, is told it is not
// arithmetic, and puts a made up number in the table instead.
var exprFuncs = map[string]struct {
	args int
	fn   func([]float64) (float64, error)
}{
	"pow": {2, func(a []float64) (float64, error) { return math.Pow(a[0], a[1]), nil }},
	"sqrt": {1, func(a []float64) (float64, error) {
		if a[0] < 0 {
			return 0, fmt.Errorf("sqrt of a negative number")
		}
		return math.Sqrt(a[0]), nil
	}},
	"abs":   {1, func(a []float64) (float64, error) { return math.Abs(a[0]), nil }},
	"round": {1, func(a []float64) (float64, error) { return math.Round(a[0]), nil }},
	"floor": {1, func(a []float64) (float64, error) { return math.Floor(a[0]), nil }},
	"ceil":  {1, func(a []float64) (float64, error) { return math.Ceil(a[0]), nil }},
	"min":   {2, func(a []float64) (float64, error) { return math.Min(a[0], a[1]), nil }},
	"max":   {2, func(a []float64) (float64, error) { return math.Max(a[0], a[1]), nil }},
}

// evalExpr runs a short program: statements separated by newlines or
// semicolons, each either `name = expression` or a bare expression, and the
// value of the last one is the answer.
//
// Naming the parts is how anybody writes a mortgage payment, and a model asked
// for one reaches for `r = 0.05/12` first. Without it the call is refused for a
// reason the model cannot act on, and it guesses instead.
func evalExpr(s string) (float64, error) {
	env := map[string]float64{}
	var last float64
	var ran bool
	for _, st := range splitStatements(s) {
		name, expr := assignment(st)
		v, err := evalOne(expr, env)
		if err != nil {
			return 0, err
		}
		if name != "" {
			env[name] = v
		}
		last, ran = v, true
	}
	if !ran {
		return 0, fmt.Errorf("there is nothing to work out here")
	}
	return last, nil
}

func splitStatements(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ';' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// assignment splits `name = expression`. A bare expression comes back with no
// name, and its value is still available as the answer if it is the last one.
func assignment(st string) (name, expr string) {
	i := strings.Index(st, "=")
	if i < 0 {
		return "", st
	}
	n := strings.TrimSpace(st[:i])
	if n == "" || !isName(n) {
		return "", st
	}
	return n, strings.TrimSpace(st[i+1:])
}

func isName(s string) bool {
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// evalOne walks a parsed Go expression rather than shelling out to anything.
// go/parser is already in the standard library and it rejects everything that
// is not an expression for free, so the whole risk surface is the node types
// allowed below.
func evalOne(s string, env map[string]float64) (float64, error) {
	s, err := powers(s)
	if err != nil {
		return 0, err
	}
	node, err := parser.ParseExpr(s)
	if err != nil {
		return 0, fmt.Errorf("that is not an arithmetic expression")
	}
	v, err := evalNode(node, env)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("that does not come out to a number")
	}
	return v, nil
}

func evalNode(n ast.Expr, env map[string]float64) (float64, error) {
	switch e := n.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT, token.FLOAT:
			return strconv.ParseFloat(e.Value, 64)
		}
		return 0, fmt.Errorf("only numbers are allowed")
	case *ast.Ident:
		v, ok := env[e.Name]
		if !ok {
			return 0, fmt.Errorf("nothing has been set called %s, write %s = ... on an earlier line", e.Name, e.Name)
		}
		return v, nil
	case *ast.ParenExpr:
		return evalNode(e.X, env)
	case *ast.CallExpr:
		name, ok := e.Fun.(*ast.Ident)
		if !ok {
			return 0, fmt.Errorf("only arithmetic is supported")
		}
		f, ok := exprFuncs[name.Name]
		if !ok {
			return 0, fmt.Errorf("there is no %s function, only pow, sqrt, abs, round, floor, ceil, min and max", name.Name)
		}
		if len(e.Args) != f.args {
			return 0, fmt.Errorf("%s takes %d argument(s), got %d", name.Name, f.args, len(e.Args))
		}
		args := make([]float64, len(e.Args))
		for i, a := range e.Args {
			v, err := evalNode(a, env)
			if err != nil {
				return 0, err
			}
			args[i] = v
		}
		return f.fn(args)
	case *ast.UnaryExpr:
		v, err := evalNode(e.X, env)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case token.SUB:
			return -v, nil
		case token.ADD:
			return v, nil
		}
		return 0, fmt.Errorf("unsupported sign")
	case *ast.BinaryExpr:
		// powers rewrites every ^ it can see, so one left here had no operand.
		if e.Op == token.XOR {
			return 0, fmt.Errorf("^ is not a power here, write pow(base, exponent)")
		}
		l, err := evalNode(e.X, env)
		if err != nil {
			return 0, err
		}
		r, err := evalNode(e.Y, env)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case token.ADD:
			return l + r, nil
		case token.SUB:
			return l - r, nil
		case token.MUL:
			return l * r, nil
		case token.QUO:
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return l / r, nil
		case token.REM:
			if int64(r) == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return float64(int64(l) % int64(r)), nil
		}
		return 0, fmt.Errorf("unsupported operator %s", e.Op)
	}
	return 0, fmt.Errorf("only arithmetic is supported")
}

// powers turns a ^ b and a ** b into pow(a, b) before Go parses anything. Go
// reads ^ as XOR and binds it looser than * and /, so it cannot be left for the
// parser, and a model refused over it spent two rounds on 1024^3.
func powers(s string) (string, error) {
	s = strings.ReplaceAll(s, "**", "^")
	for {
		// The rightmost first, since a power binds right to left.
		at := strings.LastIndex(s, "^")
		if at < 0 {
			return s, nil
		}
		ls, ok := leftOperand(s, at)
		if !ok {
			return "", fmt.Errorf("^ needs a number on each side, or write pow(base, exponent)")
		}
		re, ok := rightOperand(s, at+1)
		if !ok {
			return "", fmt.Errorf("^ needs a number on each side, or write pow(base, exponent)")
		}
		s = s[:ls] + "pow(" + strings.TrimSpace(s[ls:at]) + ", " + strings.TrimSpace(s[at+1:re]) + ")" + s[re:]
	}
}

func operandByte(c byte) bool {
	return c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// leftOperand finds where the operand ending just before i starts: a number, a
// name, a bracket, or a call like pow(2, 3).
func leftOperand(s string, i int) (int, bool) {
	j := i - 1
	for j >= 0 && s[j] == ' ' {
		j--
	}
	if j < 0 {
		return 0, false
	}
	if s[j] == ')' {
		depth := 0
		for ; j >= 0; j-- {
			switch s[j] {
			case ')':
				depth++
			case '(':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if j < 0 {
			return 0, false
		}
		for j > 0 && operandByte(s[j-1]) {
			j--
		}
		return j, true
	}
	end := j
	for j >= 0 && operandByte(s[j]) {
		j--
	}
	return j + 1, j < end
}

// rightOperand finds where the operand starting at i ends, a sign included.
func rightOperand(s string, i int) (int, bool) {
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	start := i
	for i < len(s) && operandByte(s[i]) {
		i++
	}
	if i < len(s) && s[i] == '(' {
		depth := 0
		for ; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				return i + 1, true
			}
		}
		return 0, false
	}
	return i, i > start
}
