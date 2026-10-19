package ci

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// The pins of §3.4, "The fixed form" (docs/SPEC-TABLES.md). The row ids
// and their digests are the design card's (schema-spec-pin-design-row-ids);
// this file is the §3.4 group's pins. Each fixed-tables feature row is
// carried by every column of the roadmap's fixed-tables table, so one test
// per port pins every row that names it; the compiler rows are settled by
// the one compiler and pinned in it. The informative rows (T3.4-1, -15,
// -29, -30, -39) take no pin: their marker says why.

var specPinS34PortTest = map[string]string{
	"cpp":    "test/tables/fixedform_main.cpp:fx_case",
	"c":      "test/c-tables/fixedform_fx1.c:fixed_fx1_read_own",
	"cs":     "test/cs-tables/src/FixedFormChecks.cs:TestFixedForm",
	"go":     "internal/codegen/gotable/fixedroadmap_t06_test.go:TestFixedRoadmapGoT06Bounds",
	"rust":   "test/rust-fixedform/src/main.rs:the_write",
	"java":   "test/java-fixedform/src/Main.java:fx1Write",
	"js":     "test/js-tables/fixedform.mjs:hostileByteSweep",
	"dart":   "test/dart-tables/fixedform.dart:fixedBoundsCase",
	"elixir": "test/elixir-fixedform/main.exs:start",
}

var specPinS34CompilerTest = map[string]string{
	"T3.4-17": "compiler/fixedrecordsize_test.go:TestDeclaredFixedTablePastTheCeilingKeepsFormOneAndIsNamed",
	"T3.4-18": "compiler/fixedrecordsize_test.go:TestTheCorpusMegabyteFixedTablesCompile",
	"T3.4-19": "compiler/fixedrecordsize_test.go:TestDeclaredFixedTableInsideTheCeilingCompiles",
	"T3.4-26": "internal/check/fixedtable_test.go:TestFixedTableClosureRefusals",
	"T3.4-31": "internal/check/fixedtable_test.go:TestFixedTableClosureRefusals",
	"T3.4-32": "compiler/fixedrecordsize_test.go:TestDeclaredFixedTablePastTheCeilingKeepsFormOneAndIsNamed",
	"T3.4-41": "compiler/fixedrecordsize_test.go:TestFixedRecordLimitRefusesTheCompile",
	"T3.4-45": "ir/formbyte_test.go:TestWireFormLineNamesTheLiveForms",
}

var specPinS34RowFeature = map[string]string{
	"T3.4-2":  "fixed-tables/field-evolution",
	"T3.4-3":  "fixed-tables/plan-selection",
	"T3.4-4":  "fixed-tables/plan-selection",
	"T3.4-5":  "fixed-tables/hostile-input",
	"T3.4-6":  "fixed-tables/hostile-input",
	"T3.4-7":  "fixed-tables/file-envelope",
	"T3.4-8":  "fixed-tables/file-envelope",
	"T3.4-9":  "fixed-tables/file-envelope",
	"T3.4-10": "fixed-tables/file-envelope",
	"T3.4-11": "fixed-tables/optional-values",
	"T3.4-12": "fixed-tables/definition-hash",
	"T3.4-13": "fixed-tables/hostile-input",
	"T3.4-14": "fixed-tables/hostile-input",
	"T3.4-16": "fixed-tables/fixed-closure",
	"T3.4-17": "compiler",
	"T3.4-18": "compiler",
	"T3.4-19": "compiler",
	"T3.4-20": "fixed-tables/counted-arrays",
	"T3.4-21": "fixed-tables/utf8-values",
	"T3.4-22": "fixed-tables/utf16-values",
	"T3.4-23": "fixed-tables/byte-buffers",
	"T3.4-24": "fixed-tables/union-values",
	"T3.4-25": "fixed-tables/optional-values",
	"T3.4-26": "compiler",
	"T3.4-27": "fixed-tables/live-extents",
	"T3.4-28": "fixed-tables/live-extents",
	"T3.4-31": "compiler",
	"T3.4-32": "compiler",
	"T3.4-33": "fixed-tables/compiled-plans",
	"T3.4-34": "fixed-tables/file-envelope",
	"T3.4-35": "fixed-tables/file-envelope",
	"T3.4-36": "fixed-tables/plan-selection",
	"T3.4-37": "fixed-tables/file-envelope",
	"T3.4-38": "fixed-tables/file-envelope",
	"T3.4-40": "fixed-tables/hostile-input",
	"T3.4-41": "compiler",
	"T3.4-42": "fixed-tables/file-envelope",
	"T3.4-43": "fixed-tables/file-envelope",
	"T3.4-44": "fixed-tables/file-envelope",
	"T3.4-45": "compiler",
	"T3.4-46": "fixed-tables/hostile-input",
	"T3.4-47": "fixed-tables/compiled-plans",
}

var specPinS34Digest = map[string]string{
	"T3.4-2":  "8b4d71343d24e9ee",
	"T3.4-3":  "edaf6d987c7f9762",
	"T3.4-4":  "e487156b7537ec35",
	"T3.4-5":  "42610f9503ffbc88",
	"T3.4-6":  "583f46d05e7e0921",
	"T3.4-7":  "f3a54847096b6e2c",
	"T3.4-8":  "bc29800bda2d011d",
	"T3.4-9":  "f367a8afd6321f8f",
	"T3.4-10": "c16858b281a36088",
	"T3.4-11": "63097ef5d4739c5e",
	"T3.4-12": "cdc1a84eea888ace",
	"T3.4-13": "063dfb2c8a0d9ecb",
	"T3.4-14": "5f3fe025ad724e50",
	"T3.4-16": "ba2aff8a20e45541",
	"T3.4-17": "17ab7022f86ffa98",
	"T3.4-18": "7052c1f6d174fede",
	"T3.4-19": "a158c2665cae7c4b",
	"T3.4-20": "95146bcd8c631a78",
	"T3.4-21": "29a37db4dcc87d9d",
	"T3.4-22": "91bcd67fb69c3d69",
	"T3.4-23": "fca7e47a0c096787",
	"T3.4-24": "c38d105af886cdf4",
	"T3.4-25": "7cd95d7484e887fe",
	"T3.4-26": "f492cb9978621d01",
	"T3.4-27": "35e35228c36e56d7",
	"T3.4-28": "4796f3d8277ceb86",
	"T3.4-31": "c231720b74393475",
	"T3.4-32": "b51e5a586a4f4ff0",
	"T3.4-33": "72a567036981a804",
	"T3.4-34": "b2bec7ee69490a3b",
	"T3.4-35": "0c6ea9f97711718d",
	"T3.4-36": "d16d29b8364ccbe2",
	"T3.4-37": "899982833fa1f4ab",
	"T3.4-38": "61c12c2f9a41f6e9",
	"T3.4-40": "00337ae8a7892c92",
	"T3.4-41": "d9123a2c266521a4",
	"T3.4-42": "aad74ffb7cd71af5",
	"T3.4-43": "54e5eb83e18eafcb",
	"T3.4-44": "4c29b5c9ae783674",
	"T3.4-45": "6102960b1866f385",
	"T3.4-46": "80d65667ea84eee9",
	"T3.4-47": "f12361a2552ee5d5",
}

func specPinS34Pins() []specPin {
	var out []specPin
	for row, feature := range specPinS34RowFeature {
		if feature == specCompilerPort {
			out = append(out, specPin{Row: row, Port: specCompilerPort, Test: specPinS34CompilerTest[row], Digest: specPinS34Digest[row]})
			continue
		}
		for _, port := range []string{"cpp", "c", "cs", "go", "rust", "java", "js", "dart", "elixir"} {
			out = append(out, specPin{Row: row, Port: port, Test: specPinS34PortTest[port], Digest: specPinS34Digest[row]})
		}
	}
	return out
}

var _ = registerSpecPins("internal/ci/schema_spec_pin_s34_test.go", specPinS34Pins()...)

// TestSpecPinsS34Plants is this section's own reversed witness: the real
// spec is clean, and taking the row id off one of §3.4's normative
// sentences turns the guard red with the "normative sentence without a row
// id" finding. The clean case is the green when the plant is out; the plant
// is the red line kept.
func TestSpecPinsS34Plants(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := os.DirFS(root)
	spec, err := fs.ReadFile(base, "docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- row T3.4-27 feature=fixed-tables/live-extents -->"
	if !strings.Contains(string(spec), marker) {
		t.Fatalf("the spec no longer carries %s; the plant needs a live row", marker)
	}
	cases := []struct {
		name  string
		spec  string
		wants []string
	}{
		{
			name:  "the real §3.4 is clean",
			spec:  string(spec),
			wants: nil,
		},
		{
			name:  "a fixed-form rule loses its id",
			spec:  strings.Replace(string(spec), marker, "", 1),
			wants: []string{`normative sentence without a row id`},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fsys := overlayFS{FS: base, over: map[string][]byte{"docs/SPEC-TABLES.md": []byte(c.spec)}}
			_, got := checkSpecPins(specPinsInput{fsys: fsys, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry})
			all := strings.Join(got, "\n")
			if len(c.wants) == 0 {
				if len(got) != 0 {
					t.Fatalf("the real spec must be clean, got:\n%s", all)
				}
				return
			}
			for _, w := range c.wants {
				if !strings.Contains(all, w) {
					t.Errorf("planted %q; want a finding containing %q, got:\n%s", c.name, w, all)
				}
			}
		})
	}
}
