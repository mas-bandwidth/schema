package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Results is the --results file: every leg the run exercised, and for each
// surface whether the leg left it out (absent) and each case's verdict. It is
// the input of the proof table (tools/matrix proof), which reads it rather
// than the printed matrix, so a cell names cases and not a count.
type Results struct {
	Langs   []string                            `json:"langs"`
	Results map[string]map[string]SurfaceResult `json:"results"`
}

// SurfaceResult is one leg's answer to one surface.
type SurfaceResult struct {
	Absent   bool              `json:"absent"`
	Failures int               `json:"failures"`
	Cases    map[string]string `json:"cases"`
}

func writeResults(path string, langs []string, results map[string]map[string]*result) error {
	out := Results{Langs: langs, Results: map[string]map[string]SurfaceResult{}}
	for _, lang := range langs {
		out.Results[lang] = map[string]SurfaceResult{}
		for s, r := range results[lang] {
			cases := r.cases
			if cases == nil {
				cases = map[string]string{}
			}
			out.Results[lang][s] = SurfaceResult{Absent: r.absent, Failures: len(r.failures), Cases: cases}
		}
	}
	data, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}
