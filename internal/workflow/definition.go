// Package workflow implements workflows as code: YAML documents describing a state machine of
// statuses and transitions, with CEL conditions and validators, screens and post-functions.
// Everything in this package is pure (no database), so it is easy to test.
package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"
)

var (
	categories   = []string{"todo", "in_progress", "done"}
	slugPattern  = regexp.MustCompile(`[^a-z0-9]+`)
	fieldPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,40}$`)
)

// SystemScreenFields are built-in fields a transition screen may prompt for.
var SystemScreenFields = []string{"resolution", "comment", "assignee", "priority", "storyPoints", "dueDate", "labels", "fixVersions"}

// Resolutions offered on screens and set automatically when an issue enters a done status.
var Resolutions = []string{"Done", "Won't Do", "Duplicate", "Cannot Reproduce"}

// ---------------------------------------------------------------------------
// Authored document (YAML)

type Document struct {
	Kind        string              `json:"kind,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Statuses    []StatusDoc         `json:"statuses"`
	Transitions []TransitionDoc     `json:"transitions"`
	Layout      map[string]Position `json:"layout,omitempty"`
}

type StatusDoc struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Initial  bool   `json:"initial,omitempty"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type TransitionDoc struct {
	ID            string         `json:"id,omitempty"`
	Name          string         `json:"name"`
	From          StringList     `json:"from,omitempty"`
	To            string         `json:"to"`
	Condition     string         `json:"condition,omitempty"`
	Validators    []Validator    `json:"validators,omitempty"`
	Screen        []ScreenField  `json:"screen,omitempty"`
	PostFunctions []PostFunction `json:"postFunctions,omitempty"`
}

// StringList accepts `from: To Do` as well as `from: [To Do, In Progress]`.
type StringList []string

func (l *StringList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*l = StringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("expected a status name or a list of status names")
	}
	*l = many
	return nil
}

// Validator accepts a bare CEL string or {expr, message}.
type Validator struct {
	Expr    string `json:"expr"`
	Message string `json:"message,omitempty"`
}

func (v *Validator) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v.Expr = s
		return nil
	}
	type plain Validator
	var p plain
	if err := strictUnmarshal(b, &p); err != nil {
		return err
	}
	*v = Validator(p)
	return nil
}

// ScreenField accepts `resolution` or {field: resolution, required: true}. A resolution field
// may list the resolutions it offers instead of the usual ones.
type ScreenField struct {
	Field    string   `json:"field"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// ResolutionOptions are the resolutions a screen field offers.
func (f ScreenField) ResolutionOptions() []string {
	if len(f.Options) > 0 {
		return f.Options
	}
	return Resolutions
}

func (f *ScreenField) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		f.Field = s
		return nil
	}
	type plain ScreenField
	var p plain
	if err := strictUnmarshal(b, &p); err != nil {
		return err
	}
	*f = ScreenField(p)
	return nil
}

// PostFunction runs after a successful transition. Exactly one field is set.
// assignTo / setField / clearField run inside the transition's database transaction;
// notify / webhook are enqueued as jobs in that same transaction (outbox).
type PostFunction struct {
	AssignTo   string    `json:"assignTo,omitempty"`
	SetField   *SetField `json:"setField,omitempty"`
	ClearField string    `json:"clearField,omitempty"`
	Notify     string    `json:"notify,omitempty"`
	Webhook    string    `json:"webhook,omitempty"`
}

type SetField struct {
	Field string `json:"field"`
	Value any    `json:"value,omitempty"`
}

func strictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ---------------------------------------------------------------------------
// Compiled definition (stored per version, statuses resolved to ids)

type Definition struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Statuses    []Status            `json:"statuses"`
	Transitions []Transition        `json:"transitions"`
	Layout      map[string]Position `json:"layout,omitempty"`
}

type Status struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Category string    `json:"category"`
	Initial  bool      `json:"initial"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// From is nil when the transition is available from any status.
	From          []uuid.UUID    `json:"from,omitempty"`
	To            uuid.UUID      `json:"to"`
	Condition     string         `json:"condition,omitempty"`
	Validators    []Validator    `json:"validators,omitempty"`
	Screen        []ScreenField  `json:"screen,omitempty"`
	PostFunctions []PostFunction `json:"postFunctions,omitempty"`
}

func (d *Definition) Status(id uuid.UUID) (Status, bool) {
	for _, s := range d.Statuses {
		if s.ID == id {
			return s, true
		}
	}
	return Status{}, false
}

func (d *Definition) StatusByName(name string) (Status, bool) {
	for _, s := range d.Statuses {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return Status{}, false
}

func (d *Definition) Initial() Status {
	for _, s := range d.Statuses {
		if s.Initial {
			return s
		}
	}
	return d.Statuses[0]
}

func (d *Definition) Transition(id string) (Transition, bool) {
	for _, t := range d.Transitions {
		if t.ID == id {
			return t, true
		}
	}
	return Transition{}, false
}

// From reports whether the transition can start from the given status.
func (t Transition) AppliesFrom(statusID uuid.UUID) bool {
	return t.To != statusID && (t.From == nil || slices.Contains(t.From, statusID))
}

// Outgoing lists transitions that can start from a status, ignoring conditions.
func (d *Definition) Outgoing(statusID uuid.UUID) []Transition {
	var out []Transition
	for _, t := range d.Transitions {
		if t.AppliesFrom(statusID) {
			out = append(out, t)
		}
	}
	return out
}

func (d *Definition) HasStatus(id uuid.UUID) bool {
	_, ok := d.Status(id)
	return ok
}

// ---------------------------------------------------------------------------
// Parsing & validation

type Problem struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		if x.Path != "" {
			parts[i] = x.Path + ": " + x.Message
		} else {
			parts[i] = x.Message
		}
	}
	return strings.Join(parts, "; ")
}

// Parse reads a YAML (or JSON) workflow document, rejecting unknown keys. YAML 1.2 rules
// apply, so keys such as `y` or `on` stay strings.
func Parse(src []byte) (*Document, error) {
	var generic any
	if err := yaml.Unmarshal(src, &generic); err != nil {
		return nil, Problems{{Message: "invalid YAML: " + err.Error()}}
	}
	if generic == nil {
		return nil, Problems{{Message: "the document is empty"}}
	}
	b, err := json.Marshal(generic)
	if err != nil {
		return nil, Problems{{Message: "invalid YAML: " + err.Error()}}
	}
	var doc Document
	if err := strictUnmarshal(b, &doc); err != nil {
		return nil, Problems{{Message: "invalid workflow document: " + strings.TrimPrefix(err.Error(), "json: ")}}
	}
	return &doc, nil
}

// Validate checks structure and compiles every CEL expression.
func Validate(doc *Document, ev *Evaluator) Problems {
	var probs Problems
	add := func(path, format string, args ...any) {
		probs = append(probs, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}

	if doc.Kind != "" && doc.Kind != "Workflow" {
		add("kind", "must be Workflow")
	}
	if strings.TrimSpace(doc.Name) == "" {
		add("name", "is required")
	} else if len(doc.Name) > 80 {
		add("name", "must be at most 80 characters")
	}
	if len(doc.Statuses) == 0 {
		add("statuses", "at least one status is required")
	}
	if len(doc.Statuses) > 50 {
		add("statuses", "at most 50 statuses are allowed")
	}
	names := map[string]bool{}
	initials := 0
	for i, s := range doc.Statuses {
		p := fmt.Sprintf("statuses[%d]", i)
		name := strings.TrimSpace(s.Name)
		switch {
		case name == "":
			add(p+".name", "is required")
		case len(name) > 60:
			add(p+".name", "must be at most 60 characters")
		case names[strings.ToLower(name)]:
			add(p+".name", "duplicate status %q", name)
		}
		names[strings.ToLower(name)] = true
		if !slices.Contains(categories, s.Category) {
			add(p+".category", "must be one of todo, in_progress, done")
		}
		if s.Initial {
			initials++
		}
	}
	if initials > 1 {
		add("statuses", "only one status can be marked initial")
	}

	ids := map[string]bool{}
	for i, t := range doc.Transitions {
		p := fmt.Sprintf("transitions[%d]", i)
		if strings.TrimSpace(t.Name) == "" {
			add(p+".name", "is required")
		}
		if t.ID != "" {
			if ids[t.ID] {
				add(p+".id", "duplicate transition id %q", t.ID)
			}
			ids[t.ID] = true
		}
		if !names[strings.ToLower(strings.TrimSpace(t.To))] {
			add(p+".to", "unknown status %q", t.To)
		}
		for j, f := range t.From {
			if f == "*" {
				continue
			}
			if !names[strings.ToLower(strings.TrimSpace(f))] {
				add(fmt.Sprintf("%s.from[%d]", p, j), "unknown status %q", f)
			} else if strings.EqualFold(strings.TrimSpace(f), strings.TrimSpace(t.To)) {
				add(fmt.Sprintf("%s.from[%d]", p, j), "a transition cannot start and end on the same status")
			}
		}
		if t.Condition != "" {
			if err := ev.Check(t.Condition); err != nil {
				add(p+".condition", "%v", err)
			}
		}
		for j, v := range t.Validators {
			if strings.TrimSpace(v.Expr) == "" {
				add(fmt.Sprintf("%s.validators[%d]", p, j), "expression is required")
			} else if err := ev.Check(v.Expr); err != nil {
				add(fmt.Sprintf("%s.validators[%d]", p, j), "%v", err)
			}
		}
		for j, f := range t.Screen {
			if !slices.Contains(SystemScreenFields, f.Field) && !fieldPattern.MatchString(f.Field) {
				add(fmt.Sprintf("%s.screen[%d].field", p, j), "unknown field %q", f.Field)
			}
			if len(f.Options) > 0 && f.Field != "resolution" {
				add(fmt.Sprintf("%s.screen[%d].options", p, j), "only a resolution field lists options")
			}
			for k, o := range f.Options {
				if strings.TrimSpace(o) == "" || len(o) > 60 {
					add(fmt.Sprintf("%s.screen[%d].options[%d]", p, j, k), "options must be 1 to 60 characters")
				}
			}
		}
		for j, pf := range t.PostFunctions {
			if err := validatePostFunction(pf); err != "" {
				add(fmt.Sprintf("%s.postFunctions[%d]", p, j), "%s", err)
			}
		}
	}
	for name := range doc.Layout {
		if !names[strings.ToLower(name)] {
			add("layout", "unknown status %q", name)
		}
	}
	return probs
}

func validatePostFunction(pf PostFunction) string {
	set := 0
	if pf.AssignTo != "" {
		set++
		if !slices.Contains([]string{"actor", "reporter", "unassigned"}, pf.AssignTo) && !strings.Contains(pf.AssignTo, "@") {
			return "assignTo must be actor, reporter, unassigned or an email address"
		}
	}
	if pf.SetField != nil {
		set++
		if !fieldPattern.MatchString(pf.SetField.Field) {
			return "setField.field is not a valid field name"
		}
	}
	if pf.ClearField != "" {
		set++
		if !fieldPattern.MatchString(pf.ClearField) {
			return "clearField is not a valid field name"
		}
	}
	if pf.Notify != "" {
		set++
		if !slices.Contains([]string{"assignee", "reporter", "watchers"}, pf.Notify) {
			return "notify must be assignee, reporter or watchers"
		}
	}
	if pf.Webhook != "" {
		set++
		u, err := url.Parse(pf.Webhook)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "webhook must be an http(s) URL"
		}
	}
	if set != 1 {
		return "exactly one of assignTo, setField, clearField, notify, webhook must be set"
	}
	return ""
}

// Compile resolves status names to ids (statusIDs is keyed by lower-case name) and assigns
// stable transition ids: an explicit `id`, otherwise a slug of the name.
func Compile(doc *Document, statusIDs map[string]uuid.UUID) (*Definition, error) {
	def := &Definition{Name: strings.TrimSpace(doc.Name), Description: doc.Description, Layout: doc.Layout}
	hasInitial := slices.ContainsFunc(doc.Statuses, func(s StatusDoc) bool { return s.Initial })
	for i, s := range doc.Statuses {
		name := strings.TrimSpace(s.Name)
		id, ok := statusIDs[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("status %q was not resolved", name)
		}
		def.Statuses = append(def.Statuses, Status{ID: id, Name: name, Category: s.Category, Initial: s.Initial || (!hasInitial && i == 0)})
	}
	byName := func(n string) uuid.UUID { return statusIDs[strings.ToLower(strings.TrimSpace(n))] }
	used := map[string]int{}
	for _, t := range doc.Transitions {
		if t.ID != "" {
			used[t.ID]++
		}
	}
	for _, t := range doc.Transitions {
		id := t.ID
		if id == "" {
			base := Slug(t.Name)
			id = base
			for n := 2; used[id] > 0; n++ {
				id = fmt.Sprintf("%s-%d", base, n)
			}
			used[id]++
		}
		tr := Transition{
			ID: id, Name: strings.TrimSpace(t.Name), To: byName(t.To), Condition: t.Condition,
			Validators: t.Validators, Screen: t.Screen, PostFunctions: t.PostFunctions,
		}
		if len(t.From) > 0 && !slices.Contains(t.From, "*") {
			for _, f := range t.From {
				tr.From = append(tr.From, byName(f))
			}
		}
		def.Transitions = append(def.Transitions, tr)
	}
	return def, nil
}

// Slug lower-cases and hyphenates: "In Review" -> "in-review" (also used for smart commits).
func Slug(s string) string {
	return strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// Decompile turns a stored definition back into an authored document (for export / the editor).
func Decompile(def *Definition) *Document {
	doc := &Document{Kind: "Workflow", Name: def.Name, Description: def.Description, Layout: def.Layout}
	names := map[uuid.UUID]string{}
	for _, s := range def.Statuses {
		names[s.ID] = s.Name
		doc.Statuses = append(doc.Statuses, StatusDoc{Name: s.Name, Category: s.Category, Initial: s.Initial})
	}
	for _, t := range def.Transitions {
		td := TransitionDoc{
			ID: t.ID, Name: t.Name, To: names[t.To], Condition: t.Condition,
			Validators: t.Validators, Screen: t.Screen, PostFunctions: t.PostFunctions,
		}
		for _, f := range t.From {
			td.From = append(td.From, names[f])
		}
		doc.Transitions = append(doc.Transitions, td)
	}
	return doc
}

// ToYAML renders a document as YAML, keeping the field order of the JSON form.
func ToYAML(doc *Document) (string, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(b, &node); err != nil {
		return "", err
	}
	setBlockStyle(&node)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// setBlockStyle turns JSON-derived flow style into readable block YAML, keeping small leaf
// maps (statuses, positions) on one line.
func setBlockStyle(n *yaml.Node) {
	leaf := n.Kind == yaml.MappingNode && len(n.Content) <= 6
	for _, c := range n.Content {
		if c.Kind == yaml.MappingNode || c.Kind == yaml.SequenceNode {
			leaf = false
		}
	}
	scalars := n.Kind == yaml.SequenceNode && len(n.Content) <= 6
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode {
			scalars = false
		}
	}
	if (leaf && n.Kind == yaml.MappingNode) || scalars {
		n.Style = yaml.FlowStyle
	} else {
		n.Style = 0
	}
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode && c.Style == yaml.DoubleQuotedStyle {
			c.Style = 0
		}
		setBlockStyle(c)
	}
}
