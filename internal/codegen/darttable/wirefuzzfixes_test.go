package darttable

// THE TOLERANT WIRE'S DIFFERENTIAL FUZZER FOUND THESE (docs/PORTING.md I15,
// make tables-dart-wire-fuzz). Each is a rule of docs/SPEC-TABLES.md §3 or §4
// that the generated Dart reader or writer did not keep, found as a mutant the
// engine (internal/tablewire) and the leg answered differently; the fuzzer is
// what proves the behaviour (259,457 mutants, 0 divergences), and this test is
// the cheap, toolchain-free witness that the emitter still spells each rule.
//
// Every property is checked twice. The generated source must carry the rule's
// spelling; and the REVERSED WITNESS puts the pre-fix spelling back into that
// same source and requires the same check to fail, so a check that cannot go
// red proves nothing.

import (
	"path/filepath"
	"strings"
	"testing"
)

// dartFunc is one top-level function's text, from the line that starts with
// sig to the closing brace at column zero.
func dartFunc(t *testing.T, src, sig string) string {
	t.Helper()
	i := strings.Index(src, "\n"+sig)
	if i < 0 {
		t.Fatalf("no generated function starting %q", sig)
	}
	rest := src[i+1:]
	j := strings.Index(rest, "\n}\n")
	if j < 0 {
		t.Fatalf("function %q has no closing brace", sig)
	}
	return rest[:j+3]
}

type wireRule struct {
	name string
	// text is the generated source the rule is read from
	text string
	// want must occur in text; gone must not
	want, gone string
	// revert puts the pre-fix spelling back (the reversed witness)
	revert func(string) string
}

func (r wireRule) holds(text string) bool {
	return strings.Contains(text, r.want) && (r.gone == "" || !strings.Contains(text, r.gone))
}

func TestWireFuzzFindingsAreSpelledByTheEmitter(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	gen := func(units ...string) map[string][]byte {
		files, err := Generate(loadUnit(t, units...))
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	// tables/examples is one unit: Tables, Nested, Keyed, Pack, Ranges, Wide and
	// Guarded together
	entries, _ := filepath.Glob(filepath.Join(root, "tables", "examples", "*.schema"))
	examples := gen(entries...)
	msgs, _ := filepath.Glob(filepath.Join(root, "tables", "messages", "*.schema"))
	messages := gen(msgs...)
	guarded := gen(filepath.Join(root, "test", "tables", "G1.schema"))
	listSchemas, _ := filepath.Glob(filepath.Join(root, "tables", "lists", "*.schema"))
	lists := gen(listSchemas...)

	tabledemo := string(examples["TabledemoWire.dart"])
	messagedemo := string(messages["MessagedemoWire.dart"])
	g1 := string(guarded["Tblg1Wire.dart"])
	listdemo := string(lists["ListdemoWire.dart"])
	if tabledemo == "" || messagedemo == "" || g1 == "" || listdemo == "" {
		t.Fatalf("a Wire library was not generated: tabledemo %d messagedemo %d g1 %d listdemo %d", len(tabledemo), len(messagedemo), len(g1), len(listdemo))
	}
	squadLoad := dartFunc(t, listdemo, "void squadWireLoadBody(")

	weaponLoad := dartFunc(t, tabledemo, "void weaponConfigWireLoadBody(")
	loadoutSave := dartFunc(t, tabledemo, "void loadoutConfigWireSaveBodyFields(")
	guardedSave := dartFunc(t, g1, "void guardedWireSaveBodyFields(")
	guardedNumber := dartFunc(t, g1, "void guardedNumber(")
	guardedLoad := dartFunc(t, g1, "Guarded guardedWireLoad(")
	rootLoad := dartFunc(t, tabledemo, "RootConfig rootConfigWireLoad(")
	loadoutLoad := dartFunc(t, tabledemo, "void loadoutConfigWireLoadBody(")
	toolAt := strings.Index(messagedemo, "bool _toolMessageMarksFixed(")
	if toolAt < 0 {
		t.Fatal("messagedemo has no _toolMessageMarksFixed")
	}
	toolFixed := messagedemo[toolAt:]
	toolFixed = toolFixed[:strings.Index(toolFixed, "\n}\n")+3]

	rules := []wireRule{
		{
			name: "a scalar under a kind the field does not take keeps its default (§4)",
			text: weaponLoad, want: "var d = v.damage;", gone: "var d = 0.0;",
			revert: func(s string) string { return strings.Replace(s, "var d = v.damage;", "var d = 0.0;", 1) },
		},
		{
			name: "an enum field at None rides as reference 0 and the fields after it follow (§3)",
			text: loadoutSave, want: "default: w.putLeb(0);", gone: "default: return;",
			revert: func(s string) string { return strings.Replace(s, "default: w.putLeb(0);", "default: return;", 1) },
		},
		{
			name: "a field under a false guard is not written (§3.1, §4)",
			text: guardedSave, want: "if (v.atRest) {", gone: "",
			revert: func(s string) string { return strings.Replace(s, "if (v.atRest) {", "if (true) {", 1) },
		},
		{
			name: "a pointer under a false guard is not an edge (§3.1)",
			text: guardedNumber, want: "if (v.atRest) {", gone: "",
			revert: func(s string) string { return strings.Replace(s, "if (v.atRest) {", "if (true) {", 1) },
		},
		{
			name: "a fixed-class root never scans a node table (§3.1)",
			text: rootLoad, want: "rootConfigWireLoadBody", gone: "r.scanNodeTable();",
			revert: func(s string) string { return strings.Replace(s, "r.pos = 1;", "r.scanNodeTable();\n  r.pos = 1;", 1) },
		},
		{
			name: "a variable-class root scans its node table (§3.1)",
			text: guardedLoad, want: "r.scanNodeTable();", gone: "",
			revert: func(s string) string { return strings.Replace(s, "r.scanNodeTable();", "", 1) },
		},
		{
			name: "a by-value body is reset when its id comes round again (§4)",
			text: loadoutLoad, want: "if (seenPrimary) v.primary = WeaponConfig();", gone: "",
			revert: func(s string) string {
				return strings.Replace(s, "if (seenPrimary) v.primary = WeaponConfig();", "", 1)
			},
		},
		{
			name: "a bool payload above 1 is malformed (§3)",
			text: tabledemo, want: "if (v > 1) report.malformed = true;", gone: "",
			revert: func(s string) string { return strings.Replace(s, "if (v > 1) report.malformed = true;", "", 1) },
		},
		{
			name: "a fixed array decodes in place and keeps the prefix on damage (§4)",
			text: toolFixed, want: "final slots = v.marks;", gone: "final fresh = List<",
			revert: func(s string) string {
				return strings.Replace(s, "final slots = v.marks;", "final fresh = List<Mode>.generate(2, (_) => Mode.none);\n  final slots = fresh;", 1)
			},
		},
		{
			// ci-full run 41205503830: `02 0e 02 | 0e 83`, a map header whose
			// count crosses L under a wrong element kind (the harness pins the
			// oracle's answer, TestWireFuzzMapHeaderCrossingItsLengthIsAKindMismatch)
			name: "a map's header is read against the parent, so a count past L still judges the element kind (§2.8, §3)",
			text: squadLoad, want: "final elemKind = r.readByte();", gone: "final elemKind = arr.readByte();",
			revert: func(s string) string {
				return strings.Replace(s, "final elemKind = r.readByte();", "final elemKind = arr.readByte();", 1)
			},
		},
		{
			name: "a keyed array's header is read against the parent, as the engine's keyed body reads it (§3.2)",
			text: tabledemo, want: ", int elemKind, int pairs", gone: "final pairs = p.readLeb();",
			revert: func(s string) string {
				return strings.ReplaceAll(s, ", int elemKind, int pairs", "") + "\tfinal pairs = p.readLeb();\n"
			},
		},
	}
	for _, r := range rules {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if !r.holds(r.text) {
				t.Fatalf("the emitter no longer spells this rule (want %q, gone %q)", r.want, r.gone)
			}
			// the reversed witness: the pre-fix spelling must fail the same check
			if reverted := r.revert(r.text); reverted == r.text || r.holds(reverted) {
				t.Fatalf("the check cannot tell the fixed spelling from the pre-fix one: it passed %q", r.name)
			}
		})
	}
}
