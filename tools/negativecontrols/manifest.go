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

// The two tiers a group runs in. The owner's rule for CI that runs per commit
// is one to two minutes, so a group rides the pull request exactly when it
// fits that budget, and a control that cannot fit it alone runs nightly
// instead. Every group names its tier and a group naming neither is refused: a
// typo may not quietly take a control off both workflows.
const (
	whenPullRequest = "pull-request"
	whenNightly     = "nightly"
)

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
	// When is the tier this group runs in: pull-request or nightly.
	When string `json:"when"`
	// Why says what this row's controls have in common, for a reader of the
	// plan who is not reading the workflow. A nightly row's why line carries
	// the measurement that put it there.
	Why string `json:"why,omitempty"`
	// Targets are the make targets this row runs, in one invocation.
	Targets []string `json:"targets"`
	// Seconds is what this row's controls MEASURED on the runner — the
	// `every negative control in the <name> group` step, not the job — and it
	// exists for one purpose: the matrix hands the rows to GitHub in the order
	// it renders them, and GitHub starts them in that order as runners free
	// up. On run 34350259348 the fan-out was capacity-bound (82 jobs, about
	// twenty running at once, the last row starting 179 s in), so the run's
	// wall time was set by WHICH rows were still going at the end: the 158 s
	// and 145 s rows started at 139 s and 162 s and were the last two to
	// finish. Longest first is the classic makespan heuristic, and here it
	// costs nothing and weakens nothing — the same rows run the same controls.
	//
	// A row with no number sorts FIRST, deliberately: an unmeasured row is
	// assumed heavy, because being wrong that way costs a little wall time and
	// being wrong the other way costs a tail. Nothing about correctness rides
	// on this field; a stale number is a slower run and never a weaker one.
	Seconds int `json:"seconds,omitempty"`

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

// versionedToolchains are the fields a group and a conformance registry entry
// (test/conformance/<lang>/ci.json) spell the same way, with the same version
// string, because a group's setup step and a conformance leg's setup step
// install the same toolchain. `dotnet` is not among them on purpose: the .NET
// pin lives in .github/dotnet-version and both files carry a marker instead of
// the version, so there is no version here to hold against anything.
var versionedToolchains = []string{"rust", "node", "dart", "java", "otp", "elixir"}

// toolchain returns the version this group installs for one of the fields in
// versionedToolchains, or the empty string when the group names none.
func (g Group) toolchain(field string) string {
	switch field {
	case "rust":
		return g.Rust
	case "node":
		return g.Node
	case "dart":
		return g.Dart
	case "java":
		return g.Java
	case "otp":
		return g.OTP
	case "elixir":
		return g.Elixir
	}
	return ""
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

// tiers reports the groups whose `when` is neither tier, by name. A group that
// names no tier runs on neither workflow, which is a control running nowhere
// dressed as a plan.
func (m Manifest) tiers() []string {
	var bad []string
	for _, g := range m.Groups {
		if g.When != whenPullRequest && g.When != whenNightly {
			bad = append(bad, fmt.Sprintf("%s (when %q)", g.Name, g.When))
		}
	}
	sort.Strings(bad)
	return bad
}

// matrix renders one tier's groups as the GitHub Actions matrix that tier's
// workflow expands, so the set a workflow runs is the set this file names and
// nothing else. A row carries its name and its toolchain fields; the targets
// themselves come back through `targets <group>` inside the job, which keeps a
// hundred-target line out of the matrix value.
func (m Manifest) matrix(when string) ([]byte, error) {
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
	if when != whenPullRequest && when != whenNightly {
		return nil, fmt.Errorf("%q is not a tier: the tiers are %s and %s", when, whenPullRequest, whenNightly)
	}
	// LONGEST FIRST (see Group.Seconds). The sort is stable on the name so the
	// rendered matrix is a function of the plan alone: the same file gives the
	// same order on every run, and a diff of two runs' matrices is a diff of
	// the plan.
	groups := make([]Group, 0, len(m.Groups))
	for _, g := range m.Groups {
		if g.When == when {
			groups = append(groups, g)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i].Seconds, groups[j].Seconds
		if a == 0 {
			a = 1 << 30
		}
		if b == 0 {
			b = 1 << 30
		}
		if a != b {
			return a > b
		}
		return groups[i].Name < groups[j].Name
	})
	rows := make([]row, 0, len(groups))
	for _, g := range groups {
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
