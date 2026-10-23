package workflow

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
	"cel.dev/cel-go/ext"
)

// Evaluator compiles and runs CEL expressions for transition conditions, validators and
// automation conditions. Programs are cached by source and run with a cost limit, so a
// pathological expression cannot hang a request.
//
// Variables (all maps):
//
//	issue      key, type, typeLevel, summary, description, priority, status, statusCategory,
//	           assignee, reporter (emails, "" when unset), labels, storyPoints, resolution, fields,
//	           subtasks / children [{key, summary, type, status, category}],
//	           devLinks [{kind: branch|commit|pr|pipeline, state, title, merged}],
//	           links [{type, direction, key, category}], parent {key, type, status, category} or {}
//	actor      id, email, name, type (user|automation|scm), orgRole, roles, permissions
//	transition id, name, from, to
//	event      automation trigger details (type, field, from, to, ...)
//	now        timestamp
//
// Functions: actor.hasRole(name), actor.hasPermission(name), plus the CEL strings, lists,
// sets and math extensions.
type Evaluator struct {
	env   *cel.Env
	mu    sync.RWMutex
	cache map[string]cel.Program
}

const costLimit = 100_000

func NewEvaluator() (*Evaluator, error) {
	mapType := cel.MapType(cel.StringType, cel.DynType)
	env, err := cel.NewEnv(
		cel.Variable("issue", mapType),
		cel.Variable("actor", mapType),
		cel.Variable("transition", mapType),
		cel.Variable("event", mapType),
		cel.Variable("now", cel.TimestampType),
		cel.Function("hasRole",
			cel.MemberOverload("map_has_role", []*cel.Type{mapType, cel.StringType}, cel.BoolType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val { return listContains(lhs, "roles", rhs) }))),
		cel.Function("hasPermission",
			cel.MemberOverload("map_has_permission", []*cel.Type{mapType, cel.StringType}, cel.BoolType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val { return listContains(lhs, "permissions", rhs) }))),
		ext.Strings(),
		ext.Lists(),
		ext.Sets(),
		ext.Math(),
	)
	if err != nil {
		return nil, err
	}
	return &Evaluator{env: env, cache: map[string]cel.Program{}}, nil
}

// listContains backs actor.hasRole / hasPermission. Automation actors hold every role.
func listContains(mapVal ref.Val, key string, needle ref.Val) ref.Val {
	m, ok := mapVal.(traits.Mapper)
	if !ok {
		return types.False
	}
	if all, found := m.Find(types.String("all")); found && all == types.True {
		return types.True
	}
	list, found := m.Find(types.String(key))
	if !found {
		return types.False
	}
	l, ok := list.(traits.Lister)
	if !ok {
		return types.False
	}
	return l.Contains(needle)
}

// Check compiles an expression and requires it to return a bool.
func (e *Evaluator) Check(expr string) error {
	_, err := e.program(expr)
	return err
}

func (e *Evaluator) program(expr string) (cel.Program, error) {
	e.mu.RLock()
	prg, ok := e.cache[expr]
	e.mu.RUnlock()
	if ok {
		return prg, nil
	}
	ast, iss := e.env.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("invalid expression: %w", iss.Err())
	}
	if !ast.OutputType().IsExactType(types.BoolType) && !ast.OutputType().IsExactType(types.DynType) {
		return nil, fmt.Errorf("expression must evaluate to a boolean, not %s", ast.OutputType())
	}
	prg, err := e.env.Program(ast, cel.CostLimit(costLimit), cel.InterruptCheckFrequency(100))
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if len(e.cache) > 2000 {
		e.cache = map[string]cel.Program{}
	}
	e.cache[expr] = prg
	e.mu.Unlock()
	return prg, nil
}

// Vars is the activation for an evaluation; nil maps are replaced with empty ones.
type Vars struct {
	Issue      map[string]any
	Actor      map[string]any
	Transition map[string]any
	Event      map[string]any
	Now        time.Time
}

func (v Vars) activation() map[string]any {
	orEmpty := func(m map[string]any) map[string]any {
		if m == nil {
			return map[string]any{}
		}
		return m
	}
	now := v.Now
	if now.IsZero() {
		now = time.Now()
	}
	return map[string]any{
		"issue": orEmpty(v.Issue), "actor": orEmpty(v.Actor), "transition": orEmpty(v.Transition),
		"event": orEmpty(v.Event), "now": now,
	}
}

// Eval evaluates a boolean expression. Runtime errors (e.g. a missing key) are returned, and
// callers treat them as "false" with an explanation.
func (e *Evaluator) Eval(expr string, vars Vars) (bool, error) {
	prg, err := e.program(expr)
	if err != nil {
		return false, err
	}
	out, _, err := prg.Eval(vars.activation())
	if err != nil {
		return false, fmt.Errorf("evaluating %q: %w", expr, err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression %q did not return a boolean", expr)
	}
	return b, nil
}

// ValidatorFailure is a validator that did not pass.
type ValidatorFailure struct {
	Expr    string
	Message string
}

// RunValidators returns every failing validator (all are evaluated so users see every problem).
func (e *Evaluator) RunValidators(validators []Validator, vars Vars) []ValidatorFailure {
	var failed []ValidatorFailure
	for _, v := range validators {
		ok, err := e.Eval(v.Expr, vars)
		if ok && err == nil {
			continue
		}
		msg := v.Message
		if msg == "" {
			msg = "Validation failed: " + v.Expr
		}
		if err != nil && v.Message == "" {
			msg = err.Error()
		}
		failed = append(failed, ValidatorFailure{Expr: v.Expr, Message: msg})
	}
	return failed
}

// ActorVars builds the actor map for a person.
func ActorVars(id, email, name, actorType, orgRole string, roles, permissions []string) map[string]any {
	toAny := func(in []string) []any {
		out := make([]any, len(in))
		for i, s := range in {
			out[i] = s
		}
		return out
	}
	m := map[string]any{
		"id": id, "email": email, "name": name, "type": actorType, "orgRole": orgRole,
		"roles": toAny(roles), "permissions": toAny(permissions),
	}
	if actorType == "automation" || slices.Contains(roles, "*") {
		m["all"] = true
	}
	return m
}
