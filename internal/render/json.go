package render

import (
	"encoding/json"
	"io"

	"github.com/xceg/sphinx"
)

func JSON(w io.Writer, reports []Report, o Options) error {
	if reports == nil {
		reports = []Report{}
	}
	for i := range reports {
		if reports[i].Findings == nil {
			reports[i].Findings = []sphinx.Finding{}
		}
	}
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	e.SetEscapeHTML(false)
	return e.Encode(struct {
		Tool    string   `json:"tool"`
		Version string   `json:"version"`
		Files   []Report `json:"files"`
	}{"sphinx", o.Version, reports})
}
