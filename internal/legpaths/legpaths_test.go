package legpaths

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

const root = "../.."

func load(t *testing.T) *Index {
	t.Helper()
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

// firstFile is a file under dir (dir itself when it is a file), so a
// directory a leg reads is checked through a path a diff would name.
func firstFile(t *testing.T, rel string) string {
	t.Helper()
	if !strings.HasSuffix(rel, "/") {
		return rel
	}
	found := ""
	err := filepath.WalkDir(filepath.Join(root, rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			r, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			found = filepath.ToSlash(r)
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// EVERY FILE A LEG'S TARGETS READ SELECTS THAT LEG: every make file a leg
// owns by name, and every file and directory those make files name, is
// selected for the leg. The old hand-written `case` in ci-fast.yml's plan
// missed 184 such (leg, path) pairs (make/schema-<leg>-*.mk, the
// negative-control records, tools/moofuzzhost and tools/luaufuzzhost).
func TestEveryFileALegReadsSelectsTheLeg(t *testing.T) {
	t.Parallel()
	ix := load(t)
	reads := ix.Reads()
	if len(reads) < 500 {
		t.Fatalf("the leg make files name %d paths; the tree's are well over 500, so the scan read too little", len(reads))
	}
	for _, r := range reads {
		p := firstFile(t, r[1])
		if p == "" {
			continue // an empty directory: no diff can name a file in it
		}
		if !slices.Contains(ix.Legs(p), r[0]) {
			t.Errorf("%s reads %s (%s), and a diff to it does not select %s: selected %v", r[0], r[1], p, r[0], ix.Legs(p))
		}
	}
}

// The (leg, path) misses the 2026-11-24 parity audit named, each now
// selected, plus one path per leg the tree's conventions give every leg.
func TestTheNamedPathsSelectTheirLeg(t *testing.T) {
	t.Parallel()
	ix := load(t)
	cases := map[string]string{
		"tools/moofuzzhost/main.cpp":                        "moo",
		"tools/luaufuzzhost/main.cpp":                       "luau",
		"third_party/serialize.py/tests/test_streams.py":    "python",
		"make/schema-dart-nightly-fuzz.mk":                  "dart",
		"internal/codegen/cstable/emit.go":                  "cs",
		"internal/codegen/csharp/emit.go":                   "cs",
		"internal/codegen/golang/emit.go":                   "go",
		"internal/codegen/ccommon/x.go":                     "cpp",
		"test/tables/versioning_numbers.cpp":                "cpp",
		"test/rust-fixedform/src/main.rs":                   "rust",
		"test/packet-defaults/lua/main.lua":                 "lua",
		"generated/bench/paired/java/Main.java":             "java",
		"make/conformance-handoff.py":                       "dart",
		"make/negative-controls.d/cards-python-hash-x.json": "python",
		"make/checks/cpp-clean-format.sh":                   "cpp",
	}
	for p, leg := range cases {
		if !slices.Contains(ix.Legs(p), leg) {
			t.Errorf("%s selects %v, want %s among them", p, ix.Legs(p), leg)
		}
	}
	// every make/schema-<leg>-*.mk and every negative-control record whose
	// name carries a leg selects it
	for _, glob := range []string{"make/schema-*.mk", "make/negative-controls.d/*.json"} {
		matches, err := filepath.Glob(filepath.Join(root, glob))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatalf("%s matches nothing", glob)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m) // errdiscard: m is under root by construction
			rel = filepath.ToSlash(rel)
			for _, leg := range Owners(rel) {
				if !slices.Contains(ix.Legs(rel), leg) {
					t.Errorf("%s names %s and does not select it", rel, leg)
				}
			}
		}
	}
	for _, leg := range inputfamilies.Legs {
		p := "test/conformance/" + leg + "/x"
		if got := ix.Legs(p); !slices.Equal(got, []string{leg}) {
			t.Errorf("%s selects %v, want exactly [%s]", p, got, leg)
		}
	}
	if got := ix.Legs("docs/FAQ.md"); len(got) != 0 {
		t.Errorf("an unread page selects %v", got)
	}
}

// Owners reads a leg from the words of a make path, never from a substring:
// lua is not luau, c is not cs or cpp.
func TestOwnersAreWholeWords(t *testing.T) {
	t.Parallel()
	for p, want := range map[string][]string{
		"make/luau-fuzz.mk":                      {"luau"},
		"make/lua.mk":                            {"lua"},
		"make/cs-packet-write-text.mk":           {"cs"},
		"make/checks/c-clean-format.sh":          {"c"},
		"make/conformance-cpp-cook-write-x.json": {"cpp"},
		"make/verify-vm.mk":                      nil,
		"tools/moofuzzhost/main.cpp":             nil,
	} {
		if got := Owners(p); !slices.Equal(got, want) {
			t.Errorf("Owners(%q) = %v, want %v", p, got, want)
		}
	}
}

// What a leg's make file reads is a recipe's or a prerequisite's word, never a
// comment's; a Go package it builds is go-test-touched's, never the leg's;
// and a file only an unowned make file reads selects no leg.
func TestReadsAreRecipeWordsNotCommentsOrGoPackages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tools/host/main.cpp", "")
	write("tools/said/notes.txt", "")
	write("tools/gopkg/x.go", "package gopkg\n")
	write("data/moo/vectors/a.bin", "")
	write("data/shared/b.bin", "")
	write("make/moo-host.mk", "build/host: tools/host/main.cpp\n\tgo run ./tools/gopkg data/moo/vectors # tools/said/notes.txt\n# tools/said/notes.txt\n")
	write("make/verify-all.mk", "x: data/shared/b.bin\n")
	ix, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string][]string{
		"tools/host/main.cpp":    {"moo"},
		"data/moo/vectors/a.bin": {"moo"},
		"make/moo-host.mk":       {"moo"},
		"tools/said/notes.txt":   nil,
		"tools/gopkg/x.go":       nil,
		"data/shared/b.bin":      nil,
	} {
		if got := ix.Legs(p); !slices.Equal(got, want) {
			t.Errorf("Legs(%q) = %v, want %v", p, got, want)
		}
	}
}
