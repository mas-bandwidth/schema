package ci

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// The opening of docs/SPEC-TABLES.md: the title section, "The three wires",
// "The performance ladder", "What allocates, and what never does" and §1,
// "Purpose". The row ids and their digests are the design card's
// (schema-spec-pin-design-row-ids); this file is the s0 group's pins.
//
// The opening is a compiler section: the sentences state what the one compiler
// accepts, refuses, derives and emits, so each row's port is the compiler. The
// pins below name the test that holds the sentence, in the compiler's own tree
// or in the generated-source test of the backend the sentence names; the
// harness pins (a .dart/.mjs/.java/.ex file and the function that asserts the
// property) are the design card's non-Go pin kind.

// A pin points at a test that holds the sentence.
func specPinS0Pins() []specPin {
	return []specPin{
		{Row: "T0-1", Port: "compiler", Test: "internal/check/projection_test.go:TestIdMovesUnderWireEdits", Digest: "c1038ac4a8265fbd"},
		{Row: "T3W-1", Port: "compiler", Test: "internal/check/projection_test.go:TestIdMovesUnderWireEdits", Digest: "3fff5c2477832e8a"},
		{Row: "T3W-2", Port: "compiler", Test: "internal/check/projection_test.go:TestIdIsStableUnderNonWireEdits", Digest: "5b6199d6c30b1d22"},
		{Row: "T3W-3", Port: "compiler", Test: "internal/check/fixedtable_test.go:TestFixedTableClosureRefusals", Digest: "374ade9a48954153"},
		{Row: "T3W-4", Port: "compiler", Test: "compiler/lockdeprecated_test.go:TestDeprecatedFieldKeepsItsSlot", Digest: "6c9bcf6781e42601"},
		{Row: "T3W-5", Port: "compiler", Test: "compiler/tables_test.go:TestGeneratedTableCodeAllocatesNothing", Digest: "9c4d0f5f6a373999"},
		{Row: "T3W-6", Port: "compiler", Test: "internal/check/fixedtable_test.go:TestFixedTableClosureRefusals", Digest: "0c485f123a8cb0d9"},
		{Row: "T3W-7", Port: "compiler", Test: "internal/codegen/cpptable/messageform_held_test.go:TestMessageFormHeldCPP", Digest: "7b72a475a77f05aa"},
		{Row: "T3W-8", Port: "compiler", Test: "ir/formbyte_test.go:TestWireFormLineRefusesByName", Digest: "8dbb97e10bcf5513"},
		{Row: "T3W-9", Port: "compiler", Test: "ir/formbyte_test.go:TestWireFormLineRefusesByName", Digest: "72a22c0eb194ed43"},
		{Row: "T3W-10", Port: "compiler", Test: "internal/check/projection_test.go:TestIdIsStableUnderNonWireEdits", Digest: "e851f7a8b05fa4e2"},
		{Row: "T3W-11", Port: "compiler", Test: "internal/check/fixedtable_test.go:TestTheDeclaredClassReachesTheIr", Digest: "39c7097511e82e2d"},
		{Row: "TA-1", Port: "compiler", Test: "internal/codegen/darttable/fixedroadmap_t04_test.go:TestFixedRoadmapDartT04Versions", Digest: "89d93327c51e4f96"},
		{Row: "TA-2", Port: "compiler", Test: "compiler/tables_test.go:TestTablelessTargetsRefuseTables", Digest: "dd6963cdc1d99cc4"},
		{Row: "TA-3", Port: "compiler", Test: "compiler/tablesc_test.go:TestCEmitsTableSources", Digest: "6e0ad925acedb421"},
		{Row: "TA-4", Port: "compiler", Test: "internal/codegen/ctable/fixedroadmap_t04_test.go:TestFixedRoadmapCT04Versions", Digest: "2ef5ef33d9236057"},
		{Row: "TA-5", Port: "compiler", Test: "compiler/tablesc_test.go:TestCEmitsTableSources", Digest: "602ae4b38cc46dd0"},
		{Row: "TA-7", Port: "compiler", Test: "test/conformance/elixir/driver_impl.ex:block_verdict_at", Digest: "b7daa58b290cb6bf"},
		{Row: "TA-8", Port: "compiler", Test: "test/conformance/elixir/driver_impl.ex:block_verdict_at", Digest: "2b4aaad33640d3f0"},
		{Row: "TA-9", Port: "compiler", Test: "test/js-tables/main.mjs:walkCook", Digest: "f9b15849626c96a1"},
		{Row: "TA-10", Port: "compiler", Test: "internal/codegen/rusttable/rusttable_test.go:TestCookLayoutIsAssertedAtCompileTime", Digest: "27e44d7ddc5581f4"},
		{Row: "TA-11", Port: "compiler", Test: "compiler/tablesgo_test.go:TestGoEmitsTableSources", Digest: "1e8cd31d0545c844"},
		{Row: "TA-12", Port: "compiler", Test: "compiler/tablesgo_test.go:TestGoEmitsTableSources", Digest: "3c94de4d3d95250f"},
		{Row: "TA-13", Port: "compiler", Test: "compiler/tablesjava_test.go:TestJavaEmitsTableSources", Digest: "fde999fbc426535b"},
		{Row: "TA-14", Port: "compiler", Test: "compiler/tablesjava_test.go:TestJavaDescriptorsAreSafelyPublished", Digest: "9af25a6196dcf382"},
		{Row: "TA-15", Port: "compiler", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutsPassTheReadersOwnRules", Digest: "66b608f31c32f672"},
		{Row: "TA-16", Port: "compiler", Test: "internal/tablecook/hostile_test.go:TestCookCheckRefusesEveryForgeryItNames", Digest: "a148626278828ac8"},
		{Row: "TA-17", Port: "compiler", Test: "internal/codegen/darttable/cardsform_test.go:TestOtherVariableConstructsAreRefusedByName", Digest: "8e7575987b5a49f9"},
		{Row: "TA-18", Port: "compiler", Test: "internal/codegen/darttable/fixedroadmap_t04_test.go:TestFixedRoadmapDartT04Versions", Digest: "9a70531201435070"},
		{Row: "TA-19", Port: "compiler", Test: "compiler/sharedroadmap_compile_test.go:TestSharedRoadmapCompile", Digest: "9b3b51254710b26d"},
		{Row: "TA-20", Port: "compiler", Test: "test/conformance/dart/main.dart:run", Digest: "460c39eaff93e097"},
		{Row: "TA-21", Port: "compiler", Test: "test/java-tables/src/Main.java:modeOrder", Digest: "90788f6362939a88"},
		{Row: "TA-22", Port: "compiler", Test: "test/conformance/elixir/driver_impl.ex:block_verdict_at", Digest: "ccc03803bcd333e3"},
		{Row: "TA-23", Port: "compiler", Test: "test/js-tables/main.mjs:checkForeignByteOrder", Digest: "ee0bed282185865f"},
		{Row: "TA-24", Port: "compiler", Test: "compiler/tableselixir_test.go:TestElixirEmitsTableModules", Digest: "c589c62ddd3494a3"},
		{Row: "TA-25", Port: "compiler", Test: "internal/check/fixedtable_test.go:TestABoundedPlainTableIsStillVariable", Digest: "352637b373f1a53b"},
		{Row: "TA-26", Port: "compiler", Test: "compiler/tables_test.go:TestViewFileNamesAreClaimedInEveryUnit", Digest: "0daf1b49b9863f37"},
		{Row: "TA-27", Port: "compiler", Test: "compiler/tables_test.go:TestTableRuntimeNamesAreClaimed", Digest: "b37df3c80bbb1e7e"},
		{Row: "T1-1", Port: "compiler", Test: "internal/check/tables_test.go:TestPointerRecursionIsLegal", Digest: "7e2b4aa98b5a1cc5"},
		{Row: "T1-2", Port: "compiler", Test: "internal/check/tables_test.go:TestTableRefusals", Digest: "ae19772652c55406"},
	}
}

var _ = registerSpecPins("internal/ci/schema_spec_pin_s0_test.go", specPinS0Pins()...)

// TestSpecPinsS0Plants is this section's own reversed witness: the real spec
// is clean, and taking a row id off one of its opening sentences turns the
// guard red with the "normative sentence without a row id" finding. The clean
// case is the green when the plant is out; the plant is the red line kept.
func TestSpecPinsS0Plants(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := os.DirFS(root)
	spec, err := fs.ReadFile(base, "docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- row T3W-1 feature=compiler -->"
	if !strings.Contains(string(spec), marker) {
		t.Fatalf("the spec no longer carries %s; the plant needs a live row", marker)
	}
	cases := []struct {
		name  string
		spec  string
		wants []string
	}{
		{
			name:  "the real opening is clean",
			spec:  string(spec),
			wants: nil,
		},
		{
			name: "the first wire rule loses its id",
			spec: strings.Replace(string(spec), marker, "", 1),
			// the red line: this is the finding the guard must make, named
			wants: []string{`normative sentence without a row id (section "The three wires")`},
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
