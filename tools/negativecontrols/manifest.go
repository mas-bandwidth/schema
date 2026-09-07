package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// manifestPath is the checked-in plan the CI leg runs from, relative to the
// repository root.
const manifestPath = "make/negative-controls.json"

// Manifest is the plan: every enumerated control is in exactly one group or in
// the exclusion list, and nowhere else.
type Manifest struct {
	Groups  []Group     `json:"groups"`
	Skipped []Exclusion `json:"excluded"`
}

// Group is one matrix job: a set of controls that share a toolchain and run in
// one make invocation.
//
// The toolchain fields carry the version a row's setup step installs, and they
// are the same fields the conformance matrix uses, so the workflow conditions
// each step on a field rather than on a language name. A row that names none of
// them is a BASE row: Go, the host C and C++ compilers, and the sibling
// runtimes, which every job clones.
type Group struct {
	// Name is the matrix row's name, the job's display name, and the word the
	// leg hands back to `negativecontrols targets`.
	Name string `json:"name"`
	// Why says what this row's controls have in common, for a reader of the
	// plan who is not reading the workflow.
	Why string `json:"why,omitempty"`
	// Targets are the make targets this row runs, in one invocation.
	Targets []string `json:"targets"`

	Rust   string `json:"rust,omitempty"`
	Dotnet string `json:"dotnet,omitempty"`
	Node   string `json:"node,omitempty"`
	Dart   string `json:"dart,omitempty"`
	Java   string `json:"java,omitempty"`
	OTP    string `json:"otp,omitempty"`
	Elixir string `json:"elixir,omitempty"`
}

// Exclusion is a control the leg does not run, and why. An exclusion is a
// choice somebody wrote down; the test refuses an empty reason, so a control
// cannot leave the leg silently.
type Exclusion struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// covered returns every target the manifest accounts for, and reports the
// first target that appears twice.
func (m Manifest) covered() (map[string]string, error) {
	seen := map[string]string{}
	for _, g := range m.Groups {
		for _, t := range g.Targets {
			if where, ok := seen[t]; ok {
				return nil, fmt.Errorf("%s appears in both %s and %s", t, where, g.Name)
			}
			seen[t] = g.Name
		}
	}
	for _, e := range m.Skipped {
		if where, ok := seen[e.Target]; ok {
			return nil, fmt.Errorf("%s appears in both %s and the exclusion list", e.Target, where)
		}
		seen[e.Target] = "excluded"
	}
	return seen, nil
}

// loadManifest reads the plan from the repository root.
func loadManifest(root string) (Manifest, error) {
	var m Manifest
	body, err := os.ReadFile(filepath.Join(root, manifestPath))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("%s: %w", manifestPath, err)
	}
	return m, nil
}

// reconcile holds the manifest against the makefiles. It returns the controls
// the makefiles define and the manifest does not account for, and the controls
// the manifest names and no makefile defines.
func reconcile(defs []definition, m Manifest) (missing, stale []string, err error) {
	covered, err := m.covered()
	if err != nil {
		return nil, nil, err
	}
	defined := map[string]bool{}
	for _, d := range defs {
		defined[d.Target] = true
		if _, ok := covered[d.Target]; !ok {
			missing = append(missing, d.Target)
		}
	}
	for target := range covered {
		if !defined[target] {
			stale = append(stale, target)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return missing, stale, nil
}

// matrix renders the plan's groups as the GitHub Actions matrix the leg
// expands, so the set the leg runs is the set this file names and nothing else.
// A row carries its name and its toolchain fields; the targets themselves come
// back through `targets <group>` inside the job, which keeps a 100-target line
// out of the matrix value.
func (m Manifest) matrix() ([]byte, error) {
	type row struct {
		Name   string `json:"name"`
		Rust   string `json:"rust"`
		Dotnet string `json:"dotnet"`
		Node   string `json:"node"`
		Dart   string `json:"dart"`
		Java   string `json:"java"`
		OTP    string `json:"otp"`
		Elixir string `json:"elixir"`
	}
	rows := make([]row, 0, len(m.Groups))
	for _, g := range m.Groups {
		rows = append(rows, row{
			Name:   g.Name,
			Rust:   g.Rust,
			Dotnet: g.Dotnet,
			Node:   g.Node,
			Dart:   g.Dart,
			Java:   g.Java,
			OTP:    g.OTP,
			Elixir: g.Elixir,
		})
	}
	return json.Marshal(struct {
		Include []row `json:"include"`
	}{rows})
}

// targetsOf returns one group's make targets as the single line the leg passes
// to make.
func (m Manifest) targetsOf(name string) (string, error) {
	for _, g := range m.Groups {
		if g.Name == name {
			return strings.Join(g.Targets, " "), nil
		}
	}
	return "", fmt.Errorf("no group named %q in %s", name, manifestPath)
}
