package jiraimport

import (
	"slices"
)

// Progress is what the wizard shows while a scan or import runs, and the final report.
type Progress struct {
	Phases   []Phase        `json:"phases"`
	Counters map[string]int `json:"counters"`
	Problems []Problem      `json:"problems"`
}

type Phase struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"` // pending | running | done
	Done   int    `json:"done"`
	Total  int    `json:"total"`
}

// Problem is something the admin should know about; identical problems are counted once with
// a few example issues.
type Problem struct {
	Severity string   `json:"severity"` // error | warning | info
	Kind     string   `json:"kind"`
	Project  string   `json:"project,omitempty"`
	Message  string   `json:"message"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

const (
	maxProblems = 200
	maxExamples = 5
)

var scanPhases = []Phase{
	{Key: "metadata", Label: "Projects, fields and boards"},
	{Key: "issues", Label: "Issues"},
}

var importPhases = []Phase{
	{Key: "projects", Label: "Projects"},
	{Key: "versions", Label: "Versions → releases"},
	{Key: "sprints", Label: "Sprints"},
	{Key: "issues", Label: "Issues, comments, history and attachments"},
	{Key: "relations", Label: "Parents and links"},
	{Key: "ranks", Label: "Rank order"},
	{Key: "reports", Label: "Sprint reports"},
}

func newProgress(phases []Phase) *Progress {
	p := &Progress{Phases: slices.Clone(phases), Counters: map[string]int{}, Problems: []Problem{}}
	for i := range p.Phases {
		p.Phases[i].Status = "pending"
	}
	return p
}

func (p *Progress) phase(key string) *Phase {
	for i := range p.Phases {
		if p.Phases[i].Key == key {
			return &p.Phases[i]
		}
	}
	p.Phases = append(p.Phases, Phase{Key: key, Label: key, Status: "pending"})
	return &p.Phases[len(p.Phases)-1]
}

// start marks a phase running (and the ones before it done).
func (p *Progress) start(key string) *Phase {
	for i := range p.Phases {
		if p.Phases[i].Key == key {
			if p.Phases[i].Status == "pending" {
				p.Phases[i].Status = "running"
			}
			return &p.Phases[i]
		}
		p.Phases[i].Status = "done"
	}
	return p.phase(key)
}

func (p *Progress) finish() {
	for i := range p.Phases {
		p.Phases[i].Status = "done"
		if p.Phases[i].Total < p.Phases[i].Done {
			p.Phases[i].Total = p.Phases[i].Done
		}
	}
}

func (p *Progress) count(key string, n int) {
	if p.Counters == nil {
		p.Counters = map[string]int{}
	}
	p.Counters[key] += n
}

// add records a problem, merging it with an identical earlier one.
func (p *Progress) add(severity, kind, project, message, example string) {
	for i := range p.Problems {
		q := &p.Problems[i]
		if q.Kind == kind && q.Project == project && q.Message == message {
			q.Count++
			if example != "" && len(q.Examples) < maxExamples && !slices.Contains(q.Examples, example) {
				q.Examples = append(q.Examples, example)
			}
			return
		}
	}
	if len(p.Problems) >= maxProblems {
		p.count("problemsNotListed", 1)
		return
	}
	pr := Problem{Severity: severity, Kind: kind, Project: project, Message: message, Count: 1}
	if example != "" {
		pr.Examples = []string{example}
	}
	p.Problems = append(p.Problems, pr)
}

// hasErrors reports a problem that blocks the import.
func hasErrors(probs []Problem) bool {
	return slices.ContainsFunc(probs, func(p Problem) bool { return p.Severity == "error" })
}
