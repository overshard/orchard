package tools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

// evalExpr walks a parsed Go expression rather than shelling out to anything.
// go/parser is already in the standard library and it rejects everything that
// is not an expression for free, so the whole risk surface is the node types
// allowed below.
func evalExpr(s string) (float64, error) {
	node, err := parser.ParseExpr(s)
	if err != nil {
		return 0, fmt.Errorf("that is not an arithmetic expression")
	}
	return evalNode(node)
}

func evalNode(n ast.Expr) (float64, error) {
	switch e := n.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT, token.FLOAT:
			return strconv.ParseFloat(e.Value, 64)
		}
		return 0, fmt.Errorf("only numbers are allowed")
	case *ast.ParenExpr:
		return evalNode(e.X)
	case *ast.UnaryExpr:
		v, err := evalNode(e.X)
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
		l, err := evalNode(e.X)
		if err != nil {
			return 0, err
		}
		r, err := evalNode(e.Y)
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
