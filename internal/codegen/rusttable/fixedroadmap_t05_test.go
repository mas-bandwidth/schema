package rusttable

// THE ROADMAP'S FIELD-EVOLUTION, REPORT, AND TEXT TASKS ON THE RUST LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §4.5,
// §5.2 and §5.4; docs/FIXED-FORM-VERSIONING-TESTS.md "The rows" and
// "The divergence rows"; docs/SPEC-TABLES.md §3.4 and its SLACK RULE).
// One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs —
// the shape fixedroadmap_t04_test.go states for the same reason. The rows that
// need the emitted code to RUN against the corpus are held by the
// fixedversioning_*_test.go probes, which a gate runs; the clauses those probes
// cannot read off the emitted source are the ones below.
//
// rust/E6 [verify]: renaming uses the declared identity.
//
// rust/E8 [verify]: append and deprecate under the backward-read contract.
//
// rust/R19 [verify]: a clean NEW-READS-OLD of an appended field, variant, arm,
// flag or keyed slot moves no counter at all.
//
// rust/E9 [owed]: duplicate never raised.
//
// rust/R16 [weak]: §5.4's counters exactly.
//
// rust/R18 [owed]: a clamp that cannot fire is not emitted and nothing moves.
//
// rust/C3 [verify]: text length clamp.
//
// rust/C4 [owed]: text content refuses BY NAME.
//
// rust/R24 [owed]: ill-formed text in the USED units refuses by name; the
// count is in the text's own units and the slack is not a refusal.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t05Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t05Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t05Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t05Fn(text, name string) string {
	at := strings.Index(text, "pub fn "+name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t05Has is a required substring of the emitted source, named by rule.
func t05Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t05Pair builds the versioning pair of one row and answers the emitted module
// together with both units: the reader is newSrc, handed oldSrc's locked entry
// as its oldest lineage entry, exactly as the t04 harness does.
func t05Pair(t *testing.T, oldSrc, newSrc string) (text string, older, own *ir.Unit) {
	t.Helper()
	older = unitFrom(t, oldSrc)
	own = unitFrom(t, newSrc)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	files, err := GenerateLineage(own, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t05Fixed(t, files), older, own
}

// t05Layout is one unit's OWN locked layout bytes.
func t05Layout(t *testing.T, u *ir.Unit, name string) []byte {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the fixture declares no %s table", name)
	}
	return ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
}

// ---- rust/E6 -----------------------------------------------------------------

// t05E6 holds rust/E6 to SPEC-TABLES §3.4 and the bill's identity rule: a
// `| was = "a"` rename keeps the writer's WIRE ID, so the renamed field is
// matched by the declared identity and the layout does not move. The emitted
// hash is the old build's hash and the runtime's compiler matches on
// `tc.id == mc.id` and never on a field NAME.
func t05E6(t *testing.T) {
	text, older, own := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    b int32 = 0 | was = \"a\"\n}\n")
	oldField := older.Tables["Lineage"].Fields[0]
	newField := own.Tables["Lineage"].Fields[0]
	if oldField.Name != "a" || newField.Name != "b" {
		t.Fatalf("E6: the fixture is not a rename: %q -> %q", oldField.Name, newField.Name)
	}
	if got := ir.TableFieldWireName(newField); got != "a" {
		t.Errorf("E6: the renamed field's wire name is %q, want a — the `was` alias IS the identity", got)
	}
	if ir.TableWireId(ir.TableFieldWireName(oldField)) != ir.TableWireId(ir.TableFieldWireName(newField)) {
		t.Error("E6: the rename moved the field's wire id")
	}
	// THE LAYOUT IS BYTE-IDENTICAL and the emitted hash is the older build's:
	// a rename moves no wire byte at all.
	oldLayout, newLayout := t05Layout(t, older, "Lineage"), t05Layout(t, own, "Lineage")
	if string(oldLayout) != string(newLayout) {
		t.Error("E6: the rename moved the layout bytes")
	}
	want := fmt.Sprintf("pub const LINEAGE_FIXED_HASH: u64 = 0x%016x;", ir.TableFixedLayoutHash(oldLayout, older.Tables["Lineage"]))
	t05Has(t, text, want, "E6 the emitted hash is the declared identity's")
	// AND THE COMPILER MATCHES BY ID, not by name.
	t05Has(t, string(fixedRuntimeBody), "if tc.id == mc.id {", "E6 the plan compiler matches the writer's field by its id")
}

// ---- rust/E8 -----------------------------------------------------------------

// t05E8 holds rust/E8 to the backward-read contract of SPEC-TABLES §3.4 and
// FIXED-FORM-VERSIONING-TESTS.md's `field_append` and `field_deprecate` rows:
// an APPENDED field has no plan entry, so the PREFILL lands its declared
// default, and a DEPRECATED field keeps its slot, its id and its declared
// default, so a deprecation moves the layout not at all and the older reader
// reads the newer file (a deprecation is not newer).
func t05E8(t *testing.T) {
	// APPEND: the appended `w = 77` sits in the prefill image and the reader's
	// holes are filled from it.
	text, _, _ := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n    w int32 = 77\n}\n")
	t05Has(t, text, "0x4d, 0x00, 0x00, 0x00,", "E8 the appended field's declared default rides the prefill image")
	t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "E8 the reader's holes are what the plan does not write")
	t05Has(t, text, "image[o..o + z].copy_from_slice(&LINEAGE_FIXED_DEFAULTS[o..o + z]);", "E8 an appended field lands the declared default")

	// DEPRECATE: the pair is `{a,b,c}` -> `{a,b deprecated,c}`. The layout is
	// byte-identical and the hash does NOT move, which is the row's whole
	// point: a deprecation is not newer. The slot still rides and the reader
	// still lands it.
	oldSrc := "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 5\n    c int32 = 0\n}\n"
	newSrc := "package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    b int32 = 5 | deprecated\n    c int32 = 0\n}\n"
	older, own := unitFrom(t, oldSrc), unitFrom(t, newSrc)
	if !own.Tables["Lineage"].Fields[1].Deprecated {
		t.Fatal("E8: the fixture's second field is not marked deprecated")
	}
	oldLayout, newLayout := t05Layout(t, older, "Lineage"), t05Layout(t, own, "Lineage")
	if string(oldLayout) != string(newLayout) {
		t.Error("E8: a deprecation moved the layout bytes; the slot stays forever")
	}
	if ir.TableFixedLayoutHash(oldLayout, older.Tables["Lineage"]) != ir.TableFixedLayoutHash(newLayout, own.Tables["Lineage"]) {
		t.Error("E8: a deprecation moved the hash; the older reader must still read the newer file")
	}
	depFiles, err := Generate(own)
	if err != nil {
		t.Fatalf("E8: Generate: %v", err)
	}
	dep := t05Fixed(t, depFiles)
	t05Has(t, dep, "0x05, 0x00, 0x00, 0x00,", "E8 the deprecated slot's declared default rides the prefill image")
	t05Has(t, dep, "value.b as u32).to_le_bytes()", "E8 the deprecated slot still rides the wire")
	// NOTHING COUNTS: the compile census counts only a WRITER field the reader
	// cannot name; a deprecation names the field on both sides.
	t05Has(t, string(fixedRuntimeBody), "if !named && census {", "E8 a deprecation is not a loss and nothing counts")
}

// ---- rust/R19 -----------------------------------------------------------------

// t05R19 holds rust/R19 to §5.4's last line: "a clean NEW-READS-OLD of an
// appended field, variant, arm, flag or keyed slot — every counter stays at
// zero: an append the reader knows is not an event." The appended thing is a
// reader-side field the WRITER does not carry, so the compile census (which
// counts the other direction) names nothing, and the load's holes carry the
// declared default. Each fixture is driven through the emitted module.
func t05R19(t *testing.T) {
	// A FIELD APPEND: no ranged scalar and no text, so the whole read carries
	// no counter at all — the emitted module never mentions one.
	text, _, _ := t05Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    a int32 = 0\n    seq int32 = 0\n    w int32 = 77\n}\n")
	for _, counter := range []string{"report.clamped", "report.widened", "report.unknown += 1", "report.kind_mismatch += 1"} {
		if strings.Contains(text, counter) {
			t.Errorf("R19: a clean append moved %q in the emitted read", counter)
		}
	}
	t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "R19 an appended field is a hole the prefill covers")

	// A VARIANT, AN ARM, A FLAG AND A KEYED SLOT APPEND: each is a reader-side
	// extent the writer lacks. The emitted read still carries only the compile
	// census and the prefill, and the census is the WRITER-direction one.
	rows := []struct {
		id             string
		oldSrc, newSrc string
	}{
		{"enum variant", "package probe\n\nenum Tier { Bronze, Silver, Gold }\n\nfixed table Lineage\n{\n    e Tier = Bronze\n    seq int32 = 0\n}\n",
			"package probe\n\nenum Tier { Bronze, Silver, Gold, Platinum }\n\nfixed table Lineage\n{\n    e Tier = Bronze\n    seq int32 = 0\n}\n"},
		{"union arm", "package probe\n\ntype Alpha { m int32 = 0 }\ntype Beta { n int32 = 0 }\ntype Gamma { p int32 = 0 }\n\nunion Pick\n{\n    alpha Alpha\n    beta Beta\n}\n\nfixed table Lineage\n{\n    pick Pick\n    seq int32 = 0\n}\n",
			"package probe\n\ntype Alpha { m int32 = 0 }\ntype Beta { n int32 = 0 }\ntype Gamma { p int32 = 0 }\n\nunion Pick\n{\n    alpha Alpha\n    beta Beta\n    gamma Gamma\n}\n\nfixed table Lineage\n{\n    pick Pick\n    seq int32 = 0\n}\n"},
		{"flag", "package probe\n\nflags Caps { Jump, Crouch }\n\nfixed table Lineage\n{\n    caps Caps\n    seq int32 = 0\n}\n",
			"package probe\n\nflags Caps { Jump, Crouch, Prone }\n\nfixed table Lineage\n{\n    caps Caps\n    seq int32 = 0\n}\n"},
		{"keyed slot", "package probe\n\nenum Key { a, b, c }\n\nfixed table Lineage\n{\n    slots [Key]int32\n    seq int32 = 0\n}\n",
			"package probe\n\nenum Key { a, b, c, d }\n\nfixed table Lineage\n{\n    slots [Key]int32\n    seq int32 = 0\n}\n"},
	}
	for _, row := range rows {
		text, _, _ := t05Pair(t, row.oldSrc, row.newSrc)
		t05Has(t, text, "table_fixed_holes(entries, &mut cover, &mut hole_buf);", "R19 "+row.id+": the reader's new extent is prefilled")
		if strings.Contains(text, "report.widened += 1") {
			t.Errorf("R19 %s: the append widened a value", row.id)
		}
	}
	// THE COUNTER SITES THEMSELVES: the census is the compile-time, writer-
	// direction one and lands once after the loop; widened rides only the two
	// widen ops, so no append reaches it.
	runtime := string(fixedRuntimeBody)
	t05Has(t, runtime, "if !named && census {", "R19 only a writer field the reader cannot name censuses")
	if n := strings.Count(runtime, "report.widened += 1;"); n != 2 {
		t.Errorf("R19: widened is raised at %d sites, want the two widen ops", n)
	}
	// THE CENSUS LANDS ONCE, after the record loop, on a read that RETURNS.
	load := t05Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R19: lineage_fixed_load is not emitted")
	}
	t05Has(t, load, "report.unknown += census.0;", "R19 the census lands once, after the record loop")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT05Evolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/E6", check: t05E6},
		{id: "rust/E8", check: t05E8},
		{id: "rust/R19", check: t05R19},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
