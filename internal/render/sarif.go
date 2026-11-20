package render

import (
	"encoding/json"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/xceg/sphinx"
)

const repoURL = "https://github.com/xCeg/Sphinx"

type sarifText struct {
	Text string `json:"text"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	ShortDescription     sarifText         `json:"shortDescription"`
	FullDescription      sarifText         `json:"fullDescription"`
	Help                 sarifText         `json:"help"`
	HelpURI              string            `json:"helpUri"`
	DefaultConfiguration map[string]string `json:"defaultConfiguration"`
	Properties           map[string]any    `json:"properties"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
		Region struct {
			StartLine int `json:"startLine"`
		} `json:"region"`
	} `json:"physicalLocation"`
}

func level(s sphinx.Severity) string {
	switch s {
	case sphinx.Critical, sphinx.High:
		return "error"
	case sphinx.Medium:
		return "warning"
	}
	return "note"
}

// securitySeverity feeds GitHub code scanning's severity column.
var securitySeverity = map[sphinx.Severity]string{
	sphinx.Critical: "9.5", sphinx.High: "8.0", sphinx.Medium: "5.0", sphinx.Low: "2.0",
}

func SARIF(w io.Writer, reports []Report, o Options) error {
	var rules []sarifRule
	for _, r := range sphinx.Rules() {
		rules = append(rules, sarifRule{
			ID: r.ID, Name: r.Name,
			ShortDescription:     sarifText{r.Title},
			FullDescription:      sarifText{r.Why},
			Help:                 sarifText{r.Fix},
			HelpURI:              repoURL + "/blob/main/docs/rules.md#" + strings.ToLower(r.ID),
			DefaultConfiguration: map[string]string{"level": level(r.Severity)},
			Properties:           map[string]any{"tags": []string{"privacy", "security"}, "security-severity": securitySeverity[r.Severity]},
		})
	}
	results := []sarifResult{}
	for _, rep := range reports {
		for _, f := range rep.Findings {
			res := sarifResult{RuleID: f.Rule, Level: level(f.Severity)}
			res.Message.Text = f.Message
			if f.Evidence != "" {
				res.Message.Text += " Evidence: " + f.Evidence
			}
			var loc sarifLocation
			loc.PhysicalLocation.ArtifactLocation.URI = uri(rep.Path)
			loc.PhysicalLocation.Region.StartLine = 1
			res.Locations = []sarifLocation{loc}
			results = append(results, res)
		}
	}
	doc := map[string]any{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": []any{map[string]any{
			"tool": map[string]any{"driver": map[string]any{
				"name": "Sphinx", "informationUri": repoURL, "version": o.Version, "rules": rules,
			}},
			"results": results,
		}},
	}
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	e.SetEscapeHTML(false)
	return e.Encode(doc)
}

func uri(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimPrefix(p, "./")
	return (&url.URL{Path: p}).EscapedPath()
}
