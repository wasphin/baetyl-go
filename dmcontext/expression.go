package dmcontext

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"

	"github.com/baetyl/baetyl-go/v2/errors"
)

const (
	MappingNone      = "none"
	MappingValue     = "value"
	MappingCalculate = "calculate"
)

var (
	ErrUnknownMappingType = errors.New("unknown mapping type")
)

// expression holds a parsed math expression with its variables and AST.
type expression struct {
	vars []string
	ast  ast.Node
}

// parseExpr parses a math expression string and extracts variable names.
// Only supports +, -, *, / operators and parenthesized sub-expressions.
func parseExpr(str string) (*expression, error) {
	tree, err := parser.ParseExpr(str)
	if err != nil {
		return nil, err
	}
	vars, err := extractVars(tree)
	if err != nil {
		return nil, err
	}
	return &expression{vars: vars, ast: tree}, nil
}

func extractVars(node ast.Node) (vars []string, err error) {
	switch node.(type) {
	case *ast.Ident:
		vars = []string{node.(*ast.Ident).Name}
	case *ast.BinaryExpr:
		vars, err = extractVarsBinary(node.(*ast.BinaryExpr))
	case *ast.ParenExpr:
		vars, err = extractVars(node.(*ast.ParenExpr).X)
	case *ast.BasicLit:
		// literal constant, no variables
	default:
		err = fmt.Errorf("unsupported node %+v (type %+v)", node, reflect.TypeOf(node))
	}
	return vars, err
}

func extractVarsBinary(node *ast.BinaryExpr) ([]string, error) {
	switch node.Op {
	case token.ADD, token.SUB, token.MUL, token.QUO:
	default:
		return nil, fmt.Errorf("unsupported binary operation: %s", node.Op)
	}
	lVars, err := extractVars(node.X)
	if err != nil {
		return nil, err
	}
	rVars, err := extractVars(node.Y)
	if err != nil {
		return nil, err
	}
	return append(lVars, rVars...), nil
}

// evalExpr evaluates a parsed expression with the given float64 variable scope.
func evalExpr(node ast.Node, scope map[string]float64) (float64, error) {
	switch n := node.(type) {
	case *ast.Ident:
		val, ok := scope[n.Name]
		if !ok {
			return 0, fmt.Errorf("no value for %s in scope", n.Name)
		}
		return val, nil
	case *ast.BinaryExpr:
		lVal, err := evalExpr(n.X, scope)
		if err != nil {
			return 0, err
		}
		rVal, err := evalExpr(n.Y, scope)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.ADD:
			return lVal + rVal, nil
		case token.SUB:
			return lVal - rVal, nil
		case token.MUL:
			return lVal * rVal, nil
		case token.QUO:
			return lVal / rVal, nil
		default:
			return 0, fmt.Errorf("unsupported binary operation: %s", n.Op)
		}
	case *ast.ParenExpr:
		return evalExpr(n.X, scope)
	case *ast.BasicLit:
		return strconv.ParseFloat(n.Value, 64)
	default:
		return 0, fmt.Errorf("unsupported node %+v (type %+v)", node, reflect.TypeOf(node))
	}
}

// ParseExpression parse expression string to args
// for example, input: x4/(x1+x2+x1*x3*10), output: [x4,x1,x2,x1,x3]
func ParseExpression(e string) ([]string, error) {
	if e == "" {
		return nil, nil
	}
	expr, err := parseExpr(e)
	if err != nil {
		return nil, errors.Trace(err)
	}
	vars := make([]string, 0)
	if expr.vars != nil {
		vars = expr.vars
	}
	return vars, nil
}

// ExecExpression execute expression with args and mappingType
// for example, input: ("x1+x2", '{"x1":1,"x2":2}', "calc"), output: 3
func ExecExpression(e string, args map[string]any, mappingType string) (any, error) {
	return ExecExpressionWithPrecision(e, args, mappingType, -1)
}

func ExecExpressionWithPrecision(e string, args map[string]any, mappingType string, precision int) (any, error) {
	switch mappingType {
	case MappingNone:
		return nil, nil
	case MappingValue:
		return processValueMappingWithPrecision(e, args, precision)
	case MappingCalculate:
		return processCalcMappingWithPrecision(e, args, precision)
	default:
		return nil, ErrUnknownMappingType
	}
}

func processValueMappingWithPrecision(e string, args map[string]any, precision int) (any, error) {
	// parse expression
	expr, err := parseExpr(e)
	if err != nil {
		return nil, errors.Trace(err)
	}
	// check the number of variables
	if len(expr.vars) != 1 {
		return nil, errors.New("mapping type equal can only have one variable")
	}
	// check variable exist
	if val, ok := args[expr.vars[0]]; ok {
		if precision <= 0 {
			return val, nil
		}
		originValue, err := ParseValueToFloat64(val)
		if err != nil {
			if err == ErrUnsupportedValueType {
				return val, nil
			}
			return nil, err
		}
		return strconv.ParseFloat(fmt.Sprintf("%."+strconv.Itoa(precision)+"f", originValue), 64)
	}
	return nil, errors.New("missing argument:" + expr.vars[0])
}

func processCalcMappingWithPrecision(e string, args map[string]any, precision int) (any, error) {
	// parse expression
	expr, err := parseExpr(e)
	if err != nil {
		return nil, errors.Trace(err)
	}
	// parse variable to float64
	parseArgs := map[string]float64{}
	for _, v := range expr.vars {
		if _, ok := args[v]; !ok {
			return nil, errors.New("missing variable:" + v)
		}
		val, err := ParseValueToFloat64(args[v])
		if err != nil {
			return nil, err
		}
		parseArgs[v] = val
	}
	// calculate result
	res, err := evalExpr(expr.ast, parseArgs)
	if err != nil {
		return nil, errors.Trace(err)
	}
	// format value precision
	if precision > 0 {
		return strconv.ParseFloat(fmt.Sprintf("%."+strconv.Itoa(precision)+"f", res), 64)
	}
	return res, nil
}

// SolveExpression solve the expression with value
// Note: currently only support the expression that can be simplified to ax+b
// for example, input: ((x1+1)*3+x1*2+1, 9) which means (x1+1)*3+x1*2+1=9, output: 1 which means x1=1
func SolveExpression(e string, value float64) (float64, error) {
	// parse expression
	expr, err := parseExpr(e)
	if err != nil {
		return 0, errors.Trace(err)
	}
	// check the number of variables
	set := map[string]any{}
	for _, v := range expr.vars {
		set[v] = nil
	}
	if len(set) != 1 {
		return 0, errors.New("the number of variables in expression is not one")
	}
	// simple expression
	slope, offset, err := simpleExpression(expr.ast)
	if err != nil {
		return 0, errors.Trace(err)
	}
	// solve expression
	if slope == 0 {
		return 0, errors.New("the slope is zero after simple")
	}
	return (value - offset) / slope, nil
}

// simpleExpression simple node to slope and offset
func simpleExpression(node ast.Node) (float64, float64, error) {
	switch node.(type) {
	case *ast.Ident:
		return 1, 0, nil
	case *ast.BinaryExpr:
		return processBinaryExpr(node)
	case *ast.ParenExpr:
		return simpleExpression(node.(*ast.ParenExpr).X)
	case *ast.BasicLit:
		offset, err := strconv.ParseFloat(node.(*ast.BasicLit).Value, 64)
		if err != nil {
			return 0, 0, err
		}
		return 0, offset, nil
	default:
		return 0, 0, errors.Errorf("unsupported node %+v (type %+v)", node, reflect.TypeOf(node))
	}
}

func processBinaryExpr(node ast.Node) (float64, float64, error) {
	n := node.(*ast.BinaryExpr)
	xa, xb, err := simpleExpression(n.X)
	if err != nil {
		return 0, 0, err
	}
	ya, yb, err := simpleExpression(n.Y)
	if err != nil {
		return 0, 0, err
	}
	switch n.Op {
	case token.ADD:
		return xa + ya, xb + yb, nil
	case token.SUB:
		return xa - ya, xb - yb, nil
	case token.MUL:
		if xa != 0 && ya != 0 {
			return 0, 0, errors.New("only support linear equation")
		}
		return xa*yb + xb*ya, xb * yb, nil
	case token.QUO:
		if ya != 0 {
			return 0, 0, errors.New("denominator can not have a variable")
		}
		return xa / yb, xb / yb, nil
	default:
		return 0, 0, errors.Errorf("unsupported binary operation: %s", n.Op)
	}
}
