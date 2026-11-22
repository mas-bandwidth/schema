package compiler

import (
	"slices"
	"strings"
	"testing"
)

// shippedTargets are the 18 targets in 17 languages a release ships (README.md,
// ROADMAP.md). Every one is a built-in target of the default build: a plain
// `go build ./cmd/schema`, with no build tag, answers `--lang <name>` for each.
var shippedTargets = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "goose", "java", "js",
	"lobster", "lua", "luau", "moo", "python", "ruby", "rust", "squirrel", "unity",
}

// TestShippedTargetsAreBuiltIn is the regression test for v3.0.1's defect:
// compiler/target_moo.go sat behind a build tag for its leg, so the default
// binary answered `--lang moo` with "not implemented" while the docs listed
// Moo as supported. This file carries no build tag, so it runs in the default
// build, and it fails if any shipped language is missing from the registered
// targets or has no carries registration.
func TestShippedTargetsAreBuiltIn(t *testing.T) {
	t.Parallel()
	targets := New().Targets()
	for _, name := range shippedTargets {
		if !slices.Contains(targets, name) {
			t.Errorf("--lang %s is not a registered target of the default build; targets are %v", name, targets)
		}
		if _, ok := Carried(name); !ok {
			t.Errorf("target %s has no carries registration in the default build", name)
		}
	}
}

// TestMooGeneratesInTheDefaultBuild generates a small unit, packet and table,
// with --lang moo in the default build: the packet module and the table module
// with its runtime come back.
func TestMooGeneratesInTheDefaultBuild(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\n\ntype Ping\n{\n    seq uint16\n}\n\ntable Ship\n{\n    name string(16)\n    hp int32\n}\n")
	files, err := New().Generate(u, "moo", Options{"runtime": "standalone"})
	if err != nil {
		t.Fatalf("--lang moo in the default build: %v", err)
	}
	packet, table := false, false
	for name := range files {
		if strings.HasSuffix(name, "Table.moo") {
			table = true
			continue
		}
		if strings.HasSuffix(name, ".moo") {
			packet = true
		}
	}
	if !packet || !table {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		slices.Sort(names)
		t.Fatalf("--lang moo emitted packet=%t table=%t: %v", packet, table, names)
	}
}
