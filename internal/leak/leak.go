// Package leak defines what Sphinx reports: severities, findings, and the
// catalog of rules every inspector draws from.
package leak

import (
	"fmt"
	"strings"
)

type Severity int

const (
	Low Severity = iota + 1
	Medium
	High
	Critical
)

var severityNames = map[Severity]string{
	Low:      "low",
	Medium:   "medium",
	High:     "high",
	Critical: "critical",
}

func (s Severity) String() string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return fmt.Sprintf("severity(%d)", int(s))
}

func ParseSeverity(s string) (Severity, error) {
	for sev, name := range severityNames {
		if strings.EqualFold(s, name) {
			return sev, nil
		}
	}
	return 0, fmt.Errorf("unknown severity %q (want low, medium, high or critical)", s)
}

func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Finding is one thing a file gives away. Message is written for someone who
// has never heard of metadata; Evidence is the exact value found.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Evidence string   `json:"evidence,omitempty"`
	Location string   `json:"location,omitempty"`
}

func (r *Rule) Find(msg, evidence string) Finding {
	return Finding{Rule: r.ID, Severity: r.Severity, Message: msg, Evidence: evidence}
}

func (f Finding) At(loc string) Finding {
	f.Location = loc
	return f
}

func (f Finding) Escalate(s Severity) Finding {
	f.Severity = max(f.Severity, s)
	return f
}

func (f Finding) Lower(s Severity) Finding {
	f.Severity = s
	return f
}

// Artifact is something Sphinx dug out of a file: the cropped-away pixels,
// a previous revision, the text under a black box.
type Artifact struct {
	Name string
	Note string
	Data []byte
}
