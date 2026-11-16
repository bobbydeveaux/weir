// Package spec defines the workflow contract: the shape of a definition,
// how it is parsed, and the rules it must obey before it can be published.
package spec

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ContractVersion is the only value of the top-level `weir:` key this engine accepts.
const ContractVersion = "0.1"

// Step kinds.
const (
	KindDecide        = "decide"
	KindAI            = "ai"
	KindDeterministic = "deterministic"
	KindHuman         = "human"
	KindExternal      = "external"
)

// Failure policies: what happens to the run when a step fails for good.
const (
	OnFailureBlock    = "block"
	OnFailureCancel   = "cancel"
	OnFailureContinue = "continue"
)

// Definition is one workflow.
type Definition struct {
	Weir      string  `yaml:"weir" json:"weir"`
	Namespace string  `yaml:"namespace" json:"namespace"`
	Name      string  `yaml:"name" json:"name"`
	Version   int     `yaml:"version,omitempty" json:"version,omitempty"` // informational; the engine assigns versions
	Input     *Schema `yaml:"input,omitempty" json:"input,omitempty"`
	Budget    *Budget `yaml:"budget,omitempty" json:"budget,omitempty"`
	Steps     []Step  `yaml:"steps" json:"steps"`

	// Accepted so that documents written against the full spec still parse.
	// The engine does not act on these yet; Unsupported reports any that are set.
	Triggers any `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Tiers    any `yaml:"tiers,omitempty" json:"tiers,omitempty"`
	Hooks    any `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

// Schema holds a JSON Schema, written inline in the definition.
type Schema struct {
	Schema map[string]any `yaml:"schema" json:"schema"`
}

// Budget limits spend.
type Budget struct {
	USD     float64 `yaml:"usd,omitempty" json:"usd,omitempty"`
	Minutes float64 `yaml:"minutes,omitempty" json:"minutes,omitempty"`
}

// Step is one unit of work. Which fields apply depends on Kind.
type Step struct {
	ID   string `yaml:"id" json:"id"`
	Kind string `yaml:"kind" json:"kind"`

	// The shared envelope.
	Input      map[string]any `yaml:"input,omitempty" json:"input,omitempty"`
	Output     *Schema        `yaml:"output,omitempty" json:"output,omitempty"`
	Retry      *Retry         `yaml:"retry,omitempty" json:"retry,omitempty"`
	Timeout    string         `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Next       []Next         `yaml:"next,omitempty" json:"next,omitempty"`
	OnFailure  string         `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
	Compensate string         `yaml:"compensate,omitempty" json:"compensate,omitempty"`
	End        bool           `yaml:"end,omitempty" json:"end,omitempty"`

	// decide
	Template   string   `yaml:"template,omitempty" json:"template,omitempty"`
	Labels     []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Confidence float64  `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	Default    string   `yaml:"default,omitempty" json:"default,omitempty"`

	// ai
	Prompt string  `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Tier   string  `yaml:"tier,omitempty" json:"tier,omitempty"`
	Tools  *Tools  `yaml:"tools,omitempty" json:"tools,omitempty"`
	Budget *Budget `yaml:"budget,omitempty" json:"budget,omitempty"`

	// deterministic
	Handler string `yaml:"handler,omitempty" json:"handler,omitempty"`

	// human
	Assign   *Assign      `yaml:"assign,omitempty" json:"assign,omitempty"`
	Escalate []Escalation `yaml:"escalate,omitempty" json:"escalate,omitempty"`
	Outcomes []string     `yaml:"outcomes,omitempty" json:"outcomes,omitempty"`

	// external
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"`
	Call       *Call  `yaml:"call,omitempty" json:"call,omitempty"`
	Resume     string `yaml:"resume,omitempty" json:"resume,omitempty"`
	Poll       *Poll  `yaml:"poll,omitempty" json:"poll,omitempty"`

	// Accepted but not yet acted on.
	Hooks   any `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Sandbox any `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
}

// Retry controls how many times a step is tried.
type Retry struct {
	Attempts int    `yaml:"attempts,omitempty" json:"attempts,omitempty"`
	Backoff  string `yaml:"backoff,omitempty" json:"backoff,omitempty"`
}

// Next is one routing rule. The first rule whose When is empty or true wins.
type Next struct {
	When string `yaml:"when,omitempty" json:"when,omitempty"`
	Go   string `yaml:"go" json:"go"`
}

// Tools lists what an ai step may call.
type Tools struct {
	Read    []string `yaml:"read,omitempty" json:"read,omitempty"`
	Write   []string `yaml:"write,omitempty" json:"write,omitempty"`
	Approve []string `yaml:"approve,omitempty" json:"approve,omitempty"`
}

// Assign says who a human step waits for.
type Assign struct {
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
	User string `yaml:"user,omitempty" json:"user,omitempty"`
	// Not names who may not decide. The only supported value is "run.requester".
	Not string `yaml:"not,omitempty" json:"not,omitempty"`
}

// Escalation reassigns a waiting human step after a delay.
type Escalation struct {
	After string `yaml:"after" json:"after"`
	Role  string `yaml:"role" json:"role"`
}

// Call is the request an external step makes.
type Call struct {
	Method string         `yaml:"method,omitempty" json:"method,omitempty"`
	Path   string         `yaml:"path" json:"path"`
	Body   map[string]any `yaml:"body,omitempty" json:"body,omitempty"`
}

// Poll repeats an external call until a condition holds.
type Poll struct {
	Every string `yaml:"every,omitempty" json:"every,omitempty"`
	Until string `yaml:"until" json:"until"`
}

// Parse reads a YAML definition. Unknown keys are an error, so a typo cannot
// silently disable a rule.
func Parse(source []byte) (*Definition, error) {
	dec := yaml.NewDecoder(bytes.NewReader(source))
	dec.KnownFields(true)
	var def Definition
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("parse definition: %w", err)
	}
	return &def, nil
}

// ValidateOptions supplies what Validate cannot know from the document alone.
type ValidateOptions struct {
	// HandlerExists reports whether a deterministic handler is registered.
	// When nil, handler names are not checked.
	HandlerExists func(name string) bool
}

var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	idPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	refPattern  = regexp.MustCompile(`^([a-z][a-z0-9-]*)@([1-9][0-9]*)$`)
)

// Validate checks every rule a definition must obey before it is published.
// It returns all the problems it finds, not just the first.
func (d *Definition) Validate(opts ValidateOptions) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if d.Weir != ContractVersion {
		add("weir: must be %q, got %q", ContractVersion, d.Weir)
	}
	if !namePattern.MatchString(d.Namespace) {
		add("namespace: %q must be lower-case letters, digits and hyphens, starting with a letter", d.Namespace)
	}
	if !namePattern.MatchString(d.Name) {
		add("name: %q must be lower-case letters, digits and hyphens, starting with a letter", d.Name)
	}
	if len(d.Steps) == 0 {
		add("steps: a definition needs at least one step")
	}

	ids := map[string]bool{}
	for _, s := range d.Steps {
		if ids[s.ID] {
			add("steps: id %q is used more than once", s.ID)
		}
		ids[s.ID] = true
	}

	for i := range d.Steps {
		s := &d.Steps[i]
		at := fmt.Sprintf("steps[%d] (%s)", i, s.ID)
		if !idPattern.MatchString(s.ID) {
			add("%s: id must be lower-case letters, digits and underscores, starting with a letter", at)
		}
		validateEnvelope(s, at, ids, add)
		switch s.Kind {
		case KindDecide:
			validateDecide(s, at, add)
		case KindAI:
			validateAI(s, at, add)
		case KindDeterministic:
			validateDeterministic(s, at, opts, add)
		case KindHuman:
			validateHuman(s, at, add)
		case KindExternal:
			validateExternal(s, at, add)
		default:
			add("%s: kind %q is not one of decide, ai, deterministic, human, external", at, s.Kind)
		}
	}

	if len(problems) > 0 {
		return errors.New("definition is not valid:\n  - " + strings.Join(problems, "\n  - "))
	}
	return nil
}

func validateEnvelope(s *Step, at string, ids map[string]bool, add func(string, ...any)) {
	switch s.OnFailure {
	case "", OnFailureBlock, OnFailureCancel, OnFailureContinue:
	default:
		add("%s: on_failure %q is not one of block, cancel, continue", at, s.OnFailure)
	}
	if s.Timeout != "" {
		if _, err := ParseDuration(s.Timeout); err != nil {
			add("%s: timeout: %v", at, err)
		}
	}
	if s.Retry != nil {
		if s.Retry.Attempts < 1 {
			add("%s: retry.attempts must be at least 1", at)
		}
		if s.Retry.Backoff != "" {
			if _, err := ParseDuration(s.Retry.Backoff); err != nil {
				add("%s: retry.backoff: %v", at, err)
			}
		}
	}
	for j, n := range s.Next {
		if !ids[n.Go] {
			add("%s: next[%d].go names step %q, which does not exist", at, j, n.Go)
		}
		if n.When != "" {
			if err := CheckExpression(n.When); err != nil {
				add("%s: next[%d].when: %v", at, j, err)
			}
		}
	}
	if err := checkMapping(s.Input); err != nil {
		add("%s: input: %v", at, err)
	}
	if s.Compensate != "" && s.Kind != KindDeterministic {
		add("%s: compensate is only allowed on a deterministic step", at)
	}
}

func validateDecide(s *Step, at string, add func(string, ...any)) {
	if !refPattern.MatchString(s.Template) {
		add("%s: template must name a pinned version, such as incident-route@3", at)
	}
	if len(s.Labels) < 2 {
		add("%s: labels needs at least two labels", at)
	}
	found := false
	for _, l := range s.Labels {
		if l == s.Default {
			found = true
		}
	}
	if !found {
		add("%s: default %q must be one of the labels", at, s.Default)
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		add("%s: confidence must be between 0 and 1", at)
	}
	forbid(s, at, add, "decide", fieldsExcept("template", "labels", "confidence", "default", "tier"))
}

func validateAI(s *Step, at string, add func(string, ...any)) {
	if !refPattern.MatchString(s.Prompt) {
		add("%s: prompt must name a pinned version, such as fix-incident@7", at)
	}
	if s.Output == nil || len(s.Output.Schema) == 0 {
		add("%s: an ai step must declare an output schema", at)
	}
	if s.Budget == nil || s.Budget.USD <= 0 {
		add("%s: an ai step must declare a budget in usd", at)
	}
	forbid(s, at, add, "ai", fieldsExcept("prompt", "tier", "tools", "budget"))
}

func validateDeterministic(s *Step, at string, opts ValidateOptions, add func(string, ...any)) {
	if s.Handler == "" {
		add("%s: a deterministic step must name a handler", at)
	} else if opts.HandlerExists != nil && !opts.HandlerExists(s.Handler) {
		add("%s: handler %q is not registered", at, s.Handler)
	}
	if s.Compensate != "" && opts.HandlerExists != nil && !opts.HandlerExists(s.Compensate) {
		add("%s: compensate handler %q is not registered", at, s.Compensate)
	}
	forbid(s, at, add, "deterministic", fieldsExcept("handler"))
}

func validateHuman(s *Step, at string, add func(string, ...any)) {
	if s.Assign == nil || (s.Assign.Role == "" && s.Assign.User == "") {
		add("%s: a human step must assign a role or a user", at)
	}
	if s.Assign != nil && s.Assign.Not != "" && s.Assign.Not != "run.requester" {
		add("%s: assign.not must be run.requester", at)
	}
	for j, e := range s.Escalate {
		if _, err := ParseDuration(e.After); err != nil {
			add("%s: escalate[%d].after: %v", at, j, err)
		}
		if e.Role == "" {
			add("%s: escalate[%d] must name a role", at, j)
		}
	}
	if len(s.Escalate) > 0 && s.Timeout == "" {
		add("%s: a human step with an escalation chain must also set a timeout", at)
	}
	forbid(s, at, add, "human", fieldsExcept("assign", "escalate", "outcomes"))
}

func validateExternal(s *Step, at string, add func(string, ...any)) {
	if s.Connection == "" {
		add("%s: an external step must name a connection", at)
	}
	if s.Call == nil || s.Call.Path == "" {
		add("%s: an external step must declare the call to make", at)
	} else if err := checkMapping(s.Call.Body); err != nil {
		add("%s: call.body: %v", at, err)
	}
	switch s.Resume {
	case "callback":
		if s.Poll != nil {
			add("%s: poll is only allowed with resume: poll", at)
		}
		if s.Timeout == "" {
			add("%s: a step that waits for a callback must set a timeout", at)
		}
	case "poll":
		if s.Poll == nil || s.Poll.Until == "" {
			add("%s: resume: poll needs poll.until", at)
		} else {
			if err := CheckExpression(s.Poll.Until); err != nil {
				add("%s: poll.until: %v", at, err)
			}
			if s.Poll.Every != "" {
				if _, err := ParseDuration(s.Poll.Every); err != nil {
					add("%s: poll.every: %v", at, err)
				}
			}
		}
	default:
		add("%s: resume must be callback or poll", at)
	}
	forbid(s, at, add, "external", fieldsExcept("connection", "call", "resume", "poll"))
}

// kindFields maps each kind-specific field to whether a step has set it.
func kindFields(s *Step) map[string]bool {
	return map[string]bool{
		"template":   s.Template != "",
		"labels":     len(s.Labels) > 0,
		"confidence": s.Confidence != 0,
		"default":    s.Default != "",
		"prompt":     s.Prompt != "",
		"tier":       s.Tier != "",
		"tools":      s.Tools != nil,
		"budget":     s.Budget != nil,
		"handler":    s.Handler != "",
		"assign":     s.Assign != nil,
		"escalate":   len(s.Escalate) > 0,
		"outcomes":   len(s.Outcomes) > 0,
		"connection": s.Connection != "",
		"call":       s.Call != nil,
		"resume":     s.Resume != "",
		"poll":       s.Poll != nil,
	}
}

// fieldsExcept returns every kind-specific field name apart from those allowed.
func fieldsExcept(allowed ...string) []string {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var out []string
	for name := range kindFields(&Step{}) {
		if !ok[name] {
			out = append(out, name)
		}
	}
	return out
}

// forbid reports any field set on a step whose kind does not use it.
func forbid(s *Step, at string, add func(string, ...any), kind string, names []string) {
	set := kindFields(s)
	sort.Strings(names) // a stable order for error messages
	for _, name := range names {
		if set[name] {
			add("%s: %s is not a field of a %s step", at, name, kind)
		}
	}
}

// Unsupported lists parts of the definition that parse but that this engine
// does not act on yet, so a publisher is never led to believe they are in force.
func (d *Definition) Unsupported() []string {
	var out []string
	if d.Triggers != nil {
		out = append(out, "triggers")
	}
	if d.Tiers != nil {
		out = append(out, "tiers")
	}
	if d.Hooks != nil {
		out = append(out, "hooks")
	}
	for _, s := range d.Steps {
		if s.Hooks != nil {
			out = append(out, "steps."+s.ID+".hooks")
		}
		if s.Sandbox != nil {
			out = append(out, "steps."+s.ID+".sandbox")
		}
		if s.Tools != nil {
			out = append(out, "steps."+s.ID+".tools")
		}
	}
	return out
}

// Step returns the step with the given id and its position.
func (d *Definition) Step(id string) (*Step, int) {
	for i := range d.Steps {
		if d.Steps[i].ID == id {
			return &d.Steps[i], i
		}
	}
	return nil, -1
}

// MaxAttempts is how many times a step may be tried. An ai step defaults to
// one, because every retry costs money.
func (s *Step) MaxAttempts() int {
	if s.Retry != nil && s.Retry.Attempts > 0 {
		return s.Retry.Attempts
	}
	return 1
}

// FailurePolicy is the step's on_failure, defaulting to block.
func (s *Step) FailurePolicy() string {
	if s.OnFailure == "" {
		return OnFailureBlock
	}
	return s.OnFailure
}

// HumanOutcomes is the set of outcomes a human step accepts.
func (s *Step) HumanOutcomes() []string {
	if len(s.Outcomes) > 0 {
		return s.Outcomes
	}
	return []string{"approved", "rejected"}
}

// TemplateRef splits a pinned reference such as "fix-incident@7".
func TemplateRef(ref string) (key string, version int, ok bool) {
	m := refPattern.FindStringSubmatch(ref)
	if m == nil {
		return "", 0, false
	}
	v, _ := strconv.Atoi(m[2])
	return m[1], v, true
}

// ParseDuration reads a duration such as 30s, 5m, 48h or 2d.
func ParseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("%q is not a duration", s)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a duration (use forms such as 30s, 5m, 48h or 2d)", s)
	}
	return d, nil
}
