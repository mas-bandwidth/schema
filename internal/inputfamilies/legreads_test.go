package inputfamilies

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixtureDB is a `make -p` transcript in the shape GNU make prints it: the
// variables, then the explicit targets with their prerequisites, recipes and
// the make file each recipe comes from.
const fixtureDB = `# GNU Make 4.3
# Variables

# makefile (from 'make/conformance-continues.mk', line 14)
CONTINUES_MANIFEST := test/conformance/case-outcomes/continues/manifest.txt
# makefile (from 'Makefile', line 4)
MUSTRUN := build/mustrun
# makefile (from 'Makefile', line 5)
GO_SOURCES := tools/mustrun/main.go cmd/schema/main.go

# Files

build/mustrun: tools/mustrun/main.go
#  recipe to execute (from 'Makefile', line 167):
	go build -o $@ ./tools/mustrun

java-case-outcomes-continues: build/mustrun
#  Phony target (prerequisite of .PHONY).
#  recipe to execute (from 'make/conformance-continues.mk', line 22):
	$(MUSTRUN) sh test/conformance/case-outcomes/check.sh test/conformance/java/driver $(CONTINUES_MANIFEST)
	echo "see README.md and test/conformance for the layout"

conformance-rust-framing: build/conformance-rust | build/mustrun
#  recipe to execute (from 'make/rust.mk', line 1716):
	sh test/conformance/framing/gate rust test/conformance/rust/driver build/conformance-rust-framing

build/conformance-rust: $(GO_SOURCES) test/conformance/rust/Cargo.toml
#  recipe to execute (from 'make/rust.mk', line 10):
	cargo build

bin/schema: $(GO_SOURCES)
#  recipe to execute (from 'Makefile', line 160):
	go build -o bin/schema ./cmd/schema

# files hash-table stats:
`

var fixtureTracked = []string{
	"Makefile",
	"README.md",
	"cmd/schema/main.go",
	"make/conformance-continues.mk",
	"make/rust.mk",
	"test/conformance/case-outcomes/check.sh",
	"test/conformance/case-outcomes/continues/manifest.txt",
	"test/conformance/case-outcomes/expected.txt",
	"test/conformance/framing/gate",
	"test/conformance/java/driver",
	"test/conformance/rust/Cargo.toml",
	"test/conformance/rust/driver",
	"tools/mustrun/main.go",
	"tools/mustrun/main_test.go",
}

// fixtureRoot writes the tracked files, check.sh naming expected.txt as the
// script a recipe runs.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range fixtureTracked {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "x\n"
		if f == "test/conformance/case-outcomes/check.sh" {
			body = "#!/bin/sh\ndiff \"$out\" test/conformance/case-outcomes/expected.txt\n"
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLegReadsDerivesTheReadersFromMake(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	db := ParseDB(fixtureDB)
	r := LegReads(db, NewPaths(fixtureTracked), root)
	for _, tc := range []struct {
		file string
		want []string
	}{
		// named in a leg's recipe
		{"test/conformance/framing/gate", []string{"rust"}},
		{"test/conformance/case-outcomes/check.sh", []string{"java"}},
		// named through make's own variable
		{"test/conformance/case-outcomes/continues/manifest.txt", []string{"java"}},
		// named by the script the recipe runs
		{"test/conformance/case-outcomes/expected.txt", []string{"java"}},
		// the make file a leg's recipe comes from
		{"make/conformance-continues.mk", []string{"java"}},
		// a target the leg depends on builds the directory (order-only too)
		{"tools/mustrun/main.go", []string{"rust", "java"}},
		// a test file changes no binary built from its directory
		{"tools/mustrun/main_test.go", nil},
		// prose, a parent of every leg's driver, a Go source: not reads
		{"README.md", nil},
		{"test/conformance/README.md", nil},
		{"cmd/schema/main.go", nil},
		// no leg-named target reaches bin/schema
		{"Makefile", nil},
	} {
		if got := r.Of(tc.file); !slices.Equal(got, tc.want) {
			t.Errorf("Of(%s) = %v; want %v (why: %v)", tc.file, got, tc.want, r.Why(tc.file))
		}
	}
}

// TestLegReadsNewReaderPathIsSelected is the guard the readers exist for: a
// leg target that starts reading a path no rule names selects that leg the
// moment it lands, with no list to update.
func TestLegReadsNewReaderPathIsSelected(t *testing.T) {
	t.Parallel()
	const newPath = "test/conformance/brand-new/rows.txt"
	tracked := append(slices.Clone(fixtureTracked), newPath)
	db := ParseDB(strings.Replace(fixtureDB,
		"\tsh test/conformance/framing/gate rust",
		"\tcat "+newPath+"\n\tsh test/conformance/framing/gate rust", 1))
	r := LegReads(db, NewPaths(tracked), fixtureRoot(t))
	if got := r.Of(newPath); !slices.Equal(got, []string{"rust"}) {
		t.Fatalf("a new path conformance-rust-framing reads selected %v; want [rust]", got)
	}
	if got := LegReads(ParseDB(fixtureDB), NewPaths(tracked), "").Of(newPath); got != nil {
		t.Fatalf("a path nothing reads selected %v", got)
	}
}

func TestJobReadsNamesWhatEveryRowRuns(t *testing.T) {
	t.Parallel()
	wf := "jobs:\n  plan:\n    steps:\n      - run: go run ./tools/planonly\n  verify-leg:\n    defaults:\n      run:\n        shell: go run ./tools/pgroup -- bash {0}\n    steps:\n      - run: go run ./tools/gotestreport -skips .github/skip-ledger.txt\n  verdict:\n    steps:\n      - run: go run ./tools/verdictonly\n"
	p := NewPaths([]string{"tools/pgroup/main.go", "tools/gotestreport/main.go", "tools/planonly/main.go", "tools/verdictonly/main.go", ".github/skip-ledger.txt"})
	got, err := JobReads(wf, "verify-leg", p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/skip-ledger.txt", "tools/gotestreport", "tools/pgroup"}
	if !slices.Equal(got, want) {
		t.Fatalf("JobReads = %v; want %v", got, want)
	}
	r := LegReads(DB{Rules: map[string]DBRule{}}, p, "")
	r.AddEveryLeg(got, "verify-leg")
	if legs := r.Of("tools/pgroup/main.go"); !slices.Equal(legs, Legs) {
		t.Fatalf("tools/pgroup is read by %v; want every leg", legs)
	}
	if _, err := JobReads(wf, "no-such-job", p); err == nil {
		t.Fatal("a missing job was not an error")
	}
}
