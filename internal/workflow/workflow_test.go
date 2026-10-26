package workflow

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The example from the build plan.
const planExample = `
kind: Workflow
name: software-default
statuses:
  - { name: To Do, category: todo }
  - { name: In Progress, category: in_progress }
  - { name: In Review, category: in_progress }
  - { name: Done, category: done }
transitions:
  - name: Start
    from: [To Do]
    to: In Progress
    condition: 'actor.hasRole("member")'
    postFunctions: [{ assignTo: actor }]
  - name: Submit for review
    from: [In Progress]
    to: In Review
    validators: ['issue.devLinks.exists(l, l.kind == "pr")']
  - name: Done
    from: [In Review]
    to: Done
    validators: ['issue.subtasks.all(s, s.category == "done")']
`

func compileExample(t *testing.T, src string) (*Definition, *Evaluator) {
	t.Helper()
	ev, err := NewEvaluator()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if probs := Validate(doc, ev); len(probs) > 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	ids := map[string]uuid.UUID{}
	for _, s := range doc.Statuses {
		ids[strings.ToLower(s.Name)] = uuid.New()
	}
	def, err := Compile(doc, ids)
	if err != nil {
		t.Fatal(err)
	}
	return def, ev
}

func TestPlanExampleCompiles(t *testing.T) {
	def, _ := compileExample(t, planExample)
	if def.Initial().Name != "To Do" {
		t.Errorf("initial = %q, want To Do (first status)", def.Initial().Name)
	}
	if len(def.Transitions) != 3 || def.Transitions[0].ID != "start" || def.Transitions[1].ID != "submit-for-review" {
		t.Fatalf("unexpected transitions: %+v", def.Transitions)
	}
	todo, _ := def.StatusByName("to do")
	out := def.Outgoing(todo.ID)
	if len(out) != 1 || out[0].Name != "Start" {
		t.Fatalf("outgoing from To Do = %+v", out)
	}
	if def.Transitions[0].PostFunctions[0].AssignTo != "actor" {
		t.Fatal("post-function not parsed")
	}
}

func TestConditionsAndValidators(t *testing.T) {
	def, ev := compileExample(t, planExample)
	start, review, done := def.Transitions[0], def.Transitions[1], def.Transitions[2]

	member := ActorVars("u1", "a@example.com", "A", "user", "member", []string{"member"}, nil)
	viewer := ActorVars("u2", "b@example.com", "B", "user", "member", []string{"viewer"}, nil)
	bot := ActorVars("", "", "Automation", "automation", "", nil, nil)

	for _, tc := range []struct {
		actor map[string]any
		want  bool
	}{{member, true}, {viewer, false}, {bot, true}} {
		got, err := ev.Eval(start.Condition, Vars{Actor: tc.actor})
		if err != nil || got != tc.want {
			t.Errorf("condition for %v = %v (%v), want %v", tc.actor["name"], got, err, tc.want)
		}
	}

	noPR := map[string]any{"devLinks": []any{map[string]any{"kind": "branch", "state": "active"}}}
	withPR := map[string]any{"devLinks": []any{map[string]any{"kind": "pr", "state": "open"}}}
	if f := ev.RunValidators(review.Validators, Vars{Issue: noPR}); len(f) != 1 {
		t.Errorf("expected PR validator to fail without a PR, got %v", f)
	}
	if f := ev.RunValidators(review.Validators, Vars{Issue: withPR}); len(f) != 0 {
		t.Errorf("expected PR validator to pass, got %v", f)
	}

	open := map[string]any{"subtasks": []any{map[string]any{"key": "X-2", "category": "done"}, map[string]any{"key": "X-3", "category": "todo"}}}
	closed := map[string]any{"subtasks": []any{map[string]any{"key": "X-2", "category": "done"}}}
	if f := ev.RunValidators(done.Validators, Vars{Issue: open}); len(f) != 1 {
		t.Errorf("expected sub-task validator to fail, got %v", f)
	}
	if f := ev.RunValidators(done.Validators, Vars{Issue: closed}); len(f) != 0 {
		t.Errorf("expected sub-task validator to pass, got %v", f)
	}
	// A missing key is a runtime error, reported as a failure rather than a crash.
	if f := ev.RunValidators(done.Validators, Vars{Issue: map[string]any{}}); len(f) != 1 {
		t.Errorf("expected failure for missing subtasks key, got %v", f)
	}
}

func TestValidationProblems(t *testing.T) {
	ev, _ := NewEvaluator()
	src := `
name: broken
statuses:
  - { name: Open, category: todo }
  - { name: open, category: done }
  - { name: Closed, category: finished }
transitions:
  - { name: Go, from: [Nowhere], to: Closed }
  - { name: Bad, to: Missing, condition: '"not a bool"' }
  - { name: Self, from: Open, to: Open }
  - name: PF
    to: Closed
    postFunctions: [{ assignTo: someone }, { notify: watchers, webhook: "https://example.com/hook" }]
  - name: Syntax
    to: Closed
    validators: ['issue.labels.exists(']
layout:
  Ghost: { x: 1, y: 2 }
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	probs := Validate(doc, ev)
	joined := probs.Error()
	for _, want := range []string{
		`duplicate status "open"`,
		"must be one of todo, in_progress, done",
		`unknown status "Nowhere"`,
		`unknown status "Missing"`,
		"must evaluate to a boolean",
		"cannot start and end on the same status",
		"exactly one of",
		"assignTo must be actor",
		"invalid expression",
		`layout: unknown status "Ghost"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected problem containing %q in:\n%s", want, joined)
		}
	}
}

func TestResolutionOptions(t *testing.T) {
	def, ev := compileExample(t, `
name: risk
statuses:
  - { name: Open, category: todo }
  - { name: Closed, category: done }
transitions:
  - name: Close
    to: Closed
    screen: [{ field: resolution, required: true, options: [Mitigated, Occurred] }, comment]
  - { name: Reopen, to: Open }
`)
	close, _ := def.Transition("close")
	if got := strings.Join(close.Screen[0].ResolutionOptions(), ","); got != "Mitigated,Occurred" {
		t.Fatalf("options %s", got)
	}
	if got := strings.Join(close.Screen[1].ResolutionOptions(), ","); got != strings.Join(Resolutions, ",") {
		t.Fatalf("default options %s", got)
	}
	doc, err := Parse([]byte(`
name: bad
statuses: [{ name: Open, category: todo }, { name: Closed, category: done }]
transitions:
  - { name: Close, to: Closed, screen: [{ field: comment, options: [x] }, { field: resolution, options: [""] }] }
`))
	if err != nil {
		t.Fatal(err)
	}
	joined := Validate(doc, ev).Error()
	for _, want := range []string{"only a resolution field lists options", "options must be 1 to 60 characters"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in %s", want, joined)
		}
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	_, err := Parse([]byte("name: x\nstatuses: []\ntransitions: []\nstatusez: []\n"))
	if err == nil || !strings.Contains(err.Error(), "statusez") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestAnyFromAndDuplicateNames(t *testing.T) {
	def, _ := compileExample(t, `
name: any
statuses:
  - { name: Open, category: todo }
  - { name: Closed, category: done, initial: false }
  - { name: Parked, category: todo, initial: true }
transitions:
  - { name: Reopen, from: Closed, to: Open }
  - { name: Reopen, from: Parked, to: Open }
  - { name: Close, to: Closed }
  - { name: Park, from: "*", to: Parked }
`)
	if def.Initial().Name != "Parked" {
		t.Errorf("explicit initial ignored: %s", def.Initial().Name)
	}
	if def.Transitions[0].ID != "reopen" || def.Transitions[1].ID != "reopen-2" {
		t.Errorf("ids = %s, %s", def.Transitions[0].ID, def.Transitions[1].ID)
	}
	if def.Transitions[2].From != nil || def.Transitions[3].From != nil {
		t.Error("omitted from and \"*\" should mean any status")
	}
	closed, _ := def.StatusByName("Closed")
	out := def.Outgoing(closed.ID)
	if len(out) != 2 { // Reopen + Park; Close is excluded because it targets the current status
		t.Errorf("outgoing from Closed = %+v", out)
	}
}

func TestRoundTripYAML(t *testing.T) {
	def, ev := compileExample(t, planExample)
	src, err := ToYAML(Decompile(def))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if probs := Validate(doc, ev); len(probs) > 0 {
		t.Fatalf("round-tripped YAML invalid: %v\n%s", probs, src)
	}
	if len(doc.Transitions) != 3 || doc.Transitions[0].From[0] != "To Do" {
		t.Fatalf("round trip lost data:\n%s", src)
	}
}

func TestCostLimit(t *testing.T) {
	ev, _ := NewEvaluator()
	big := make([]any, 2000)
	for i := range big {
		big[i] = i
	}
	_, err := ev.Eval(`issue.labels.all(a, issue.labels.all(b, issue.labels.all(c, true)))`, Vars{Issue: map[string]any{"labels": big}})
	if err == nil || !strings.Contains(err.Error(), "cost") {
		t.Fatalf("expected cost limit error, got %v", err)
	}
}
