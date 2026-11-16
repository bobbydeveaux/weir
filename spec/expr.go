package spec

import (
	"fmt"
	"reflect"
	"sync"

	"cel.dev/cel-go/cel"
	"google.golang.org/protobuf/types/known/structpb"
)

// Expressions in a definition are written in CEL. They may refer to:
//
//	input     the run's input
//	steps     earlier steps by id, each with .output, .label and .outcome
//	run       the run itself: run.id and run.requester
//	output    the output of the step that has just finished
//	label     its label, for a decide step
//	outcome   its outcome, for a human step
//	response  the reply to an external step's call, in poll.until
var exprEnv = sync.OnceValues(func() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("input", cel.DynType),
		cel.Variable("steps", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("run", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("output", cel.DynType),
		cel.Variable("label", cel.StringType),
		cel.Variable("outcome", cel.StringType),
		cel.Variable("response", cel.DynType),
	)
})

var programs sync.Map // expression text -> cel.Program

func program(expr string) (cel.Program, error) {
	if p, ok := programs.Load(expr); ok {
		return p.(cel.Program), nil
	}
	env, err := exprEnv()
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("%q: %w", expr, issues.Err())
	}
	p, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", expr, err)
	}
	programs.Store(expr, p)
	return p, nil
}

// CheckExpression reports whether expr compiles.
func CheckExpression(expr string) error {
	_, err := program(expr)
	return err
}

// Scope is what an expression can see.
type Scope struct {
	Input    any
	Steps    map[string]any
	Run      map[string]any
	Output   any
	Label    string
	Outcome  string
	Response any
}

func (s Scope) vars() map[string]any {
	steps, run := s.Steps, s.Run
	if steps == nil {
		steps = map[string]any{}
	}
	if run == nil {
		run = map[string]any{}
	}
	return map[string]any{
		"input": s.Input, "steps": steps, "run": run, "output": s.Output,
		"label": s.Label, "outcome": s.Outcome, "response": s.Response,
	}
}

// Eval evaluates expr and returns a value made only of JSON types.
func Eval(expr string, scope Scope) (any, error) {
	p, err := program(expr)
	if err != nil {
		return nil, err
	}
	out, _, err := p.Eval(scope.vars())
	if err != nil {
		return nil, fmt.Errorf("%q: %w", expr, err)
	}
	native, err := out.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
	if err != nil {
		return nil, fmt.Errorf("%q: result cannot be represented as JSON: %w", expr, err)
	}
	return native.(*structpb.Value).AsInterface(), nil
}

// EvalBool evaluates expr and requires a true or false result.
func EvalBool(expr string, scope Scope) (bool, error) {
	v, err := Eval(expr, scope)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%q: must be true or false, got %T", expr, v)
	}
	return b, nil
}

// checkMapping confirms every expression in a mapping compiles.
func checkMapping(m map[string]any) error {
	return walk(m, func(expr string) (any, error) { return nil, CheckExpression(expr) }, nil)
}

// Resolve evaluates a mapping. Every string in it is an expression; numbers,
// booleans, lists and nested maps are kept as written, with their strings
// evaluated in turn. To pass a fixed string, quote it: '"hello"'.
func Resolve(m map[string]any, scope Scope) (map[string]any, error) {
	out := map[string]any{}
	err := walk(m, func(expr string) (any, error) { return Eval(expr, scope) }, out)
	return out, err
}

func walk(m map[string]any, each func(string) (any, error), into map[string]any) error {
	for key, value := range m {
		v, err := walkValue(value, each)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if into != nil {
			into[key] = v
		}
	}
	return nil
}

func walkValue(value any, each func(string) (any, error)) (any, error) {
	switch v := value.(type) {
	case string:
		return each(v)
	case map[string]any:
		out := map[string]any{}
		for k, inner := range v {
			r, err := walkValue(inner, each)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, inner := range v {
			r, err := walkValue(inner, each)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = r
		}
		return out, nil
	default:
		return value, nil
	}
}
