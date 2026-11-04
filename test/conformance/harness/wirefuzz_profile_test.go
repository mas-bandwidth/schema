package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWireFuzzSkippedVectorKeepsPriorSeedMutable(t *testing.T) {
	m, _, _ := corpus(t)
	all, roots, index, _, err := buildWireFuzzRoster(m, wireFuzzOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var excluded *wireSeed
	for _, s := range all {
		r := roots[index[s]]
		if !r.variable || r.message {
			excluded = s
			break
		}
	}
	if excluded == nil {
		t.Fatal("full corpus has no outside-arm seed")
	}
	dir := t.TempDir()
	wire := filepath.Join(dir, "outside.bin")
	vectors := filepath.Join(dir, "INDEX.txt")
	if err = os.WriteFile(wire, excluded.wire, 0600); err != nil {
		t.Fatal(err)
	}
	suffix := ""
	if excluded.message {
		suffix = " message"
	}
	if err = os.WriteFile(vectors, fmt.Appendf(nil, "outside %s %s %s%s\n", excluded.unit, excluded.root, wire, suffix), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts wireFuzzOptions
	}{
		{"builder", wireFuzzOptions{builder: true}},
		{"retain", wireFuzzOptions{retain: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _, _, skipped, err := buildWireFuzzRoster(m, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			tc.opts.vectors = vectors
			after, _, _, gotSkipped, err := buildWireFuzzRoster(m, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) || gotSkipped != skipped+1 {
				t.Fatalf("outside-arm vector changed seed count: %d/%d skipped %d/%d", len(after), len(before), gotSkipped, skipped)
			}
			for i, s := range after {
				if s.name != before[i].name || s.vector {
					t.Fatalf("excluded vector changed live seed %q into vector=%v", s.name, s.vector)
				}
			}
		})
	}
}

func TestWireFuzzMixedBuilderPreservesFullRoster(t *testing.T) {
	t.Skip("layer 3: the pipe answer's malformed flag and the builder-over-region count-cap precedence, retired per docs/plans/strict-only.md (b)")
	m, _, _ := corpus(t)
	plain, roots, index, skipped, err := buildWireFuzzRoster(m, wireFuzzOptions{vectors: defaultVectors})
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Fatalf("default roster skipped %d seeds", skipped)
	}
	mixed, mixedRoots, mixedIndex, mixedSkipped, err := buildWireFuzzRoster(m, wireFuzzOptions{vectors: defaultVectors, mixedBuilder: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed) != len(plain) || len(mixedRoots) != len(roots) || mixedSkipped != 0 {
		t.Fatalf("mixed profile dropped coverage: seeds %d/%d roots %d/%d skipped%d", len(mixed), len(plain), len(mixedRoots), len(roots), mixedSkipped)
	}
	classes := map[string]bool{}
	for i, s := range plain {
		got := mixed[i]
		a, b := roots[index[s]], mixedRoots[mixedIndex[got]]
		if s.name != got.name || s.unit != got.unit || s.root != got.root || s.message != got.message || s.vector != got.vector {
			t.Fatalf("mixed seed %d changed identity or mutation policy", i)
		}
		if b.builder != (b.variable && !b.message) {
			t.Fatalf("mixed root %s.%s has wrong oracle", b.unit, b.root)
		}
		class := "fixed"
		if b.message {
			class = "message"
		} else if b.variable {
			class = "variable"
		}
		if !classes[class] && class != "variable" {
			want, err := a.oracle(s.wire)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := b.oracle(got.wire)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("%s oracle changed: %+v != %+v", class, actual, want)
			}
		}
		classes[class] = true
	}
	if len(classes) != 3 {
		t.Fatalf("corpus did not exercise all three reader classes: %v", classes)
	}
	var countRoot *wireRoot
	for _, r := range mixedRoots {
		if r.unit == "listdemo" && r.root == "Album" && !r.message {
			countRoot = r
			break
		}
	}
	if countRoot == nil {
		t.Fatal("mutable count-cap root absent")
	}
	data, err := os.ReadFile("testdata/wire/tables/fuzz-vectors/list_count_cross_length.bin")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := countRoot.oracle(data)
	if err != nil {
		t.Fatal(err)
	}
	if answer.report.Malformed || !answer.builderStopped || !answer.encFail {
		t.Fatalf("mixed variable oracle used region precedence: %+v", answer)
	}
}

func TestWireFuzzMixedBuilderRetainPreservesRetentionDomain(t *testing.T) {
	m, _, _ := corpus(t)
	base, roots, _, skipped, err := buildWireFuzzRoster(m, wireFuzzOptions{retain: true, vectors: defaultVectors})
	if err != nil {
		t.Fatal(err)
	}
	got, mixedRoots, _, gotSkipped, err := buildWireFuzzRoster(m, wireFuzzOptions{retain: true, mixedBuilder: true, vectors: defaultVectors})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(base) || len(mixedRoots) != len(roots) || skipped != gotSkipped {
		t.Fatal("mixed retain changed retention domain")
	}
	for i, s := range got {
		if s.name != base[i].name || s.vector != base[i].vector {
			t.Fatal("mixed retain changed seed policy")
		}
	}
	for _, r := range mixedRoots {
		if !r.variable || r.message || !r.builder || !r.retain {
			t.Fatalf("wrong mixed retaining oracle %+v", r)
		}
	}
	if err := wireFuzz(m, wireFuzzOptions{builder: true, mixedBuilder: true}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("ambiguous profiles accepted: %v", err)
	}
}
