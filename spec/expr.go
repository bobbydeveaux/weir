package spec

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"reflect"
	"sync"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
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
//	event     the event being matched, in a trigger: event.type, event.id, event.source, event.data
//
// and may call:
//
//	sample(seed, rate)  true for about rate of seeds, and always the same for the same seed
var exprEnv = sync.OnceValues(func() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("input", cel.DynType),
		cel.Variable("steps", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("run", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("output", cel.DynType),
		cel.Variable("label", cel.StringType),
		cel.Variable("outcome", cel.StringType),
		cel.Variable("response", cel.DynType),
		cel.Variable("event", cel.DynType),
		cel.Function("sample",
			cel.Overload("sample_string_double", []*cel.Type{cel.StringType, cel.DoubleType}, cel.BoolType,
				cel.BinaryBinding(func(seed, rate ref.Val) ref.Val {
					return sample(string(seed.(types.String)), float64(rate.(types.Double)))
				})),
			// A whole number is how 0 and 1 are usually written.
			cel.Overload("sample_string_int", []*cel.Type{cel.StringType, cel.IntType}, cel.BoolType,
				cel.BinaryBinding(func(seed, rate ref.Val) ref.Val {
					return sample(string(seed.(types.String)), float64(rate.(types.Int)))
				})),
		),
	)
})

// sample picks about rate of all seeds, and the same ones every time.
//
// It is how a workflow spot-checks work an agent is trusted to sign off: most
// runs go straight on, and a fraction go to a person. The choice is made from
// the seed alone, with no chance in it. That is deliberate: a run that is
// retried is picked or not exactly as before, so a retry cannot dodge a
// review, and anyone can work out afterwards why a run was or was not picked.
//
// A workflow that samples at two steps gives each its own seed, such as
// run.id + "/approve", or the same runs would be picked at both.
func sample(seed string, rate float64) ref.Val {
	if rate < 0 || rate > 1 || rate != rate {
		return types.NewErr("sample: the rate must be between 0 and 1, and %v is not", rate)
	}
	sum := sha256.Sum256([]byte(seed))
	// The first eight bytes as a fraction of the largest value they can hold:
	// a number from 0 up to, but never reaching, 1.
	at := float64(binary.BigEndian.Uint64(sum[:8])>>11) / (1 << 53)
	return types.Bool(at < rate)
}

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
	Event    any
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
		"label": s.Label, "outcome": s.Outcome, "response": s.Response, "event": s.Event,
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
