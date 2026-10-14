// The SHARED gate of the NEW Fixed Tables work set for the compiler side of the
// plan (docs/roadmap.sexp, node "shared"): shared/S5, the four compiler-side
// record and plan bounds, and shared/S6, the generator-side plan laid down as
// data that every leg's emitter reads.  One subtest per task id, table-driven,
// t.Parallel() first.  A clause the tree already holds also names the dedicated
// test that holds it, so this file is the gate index and the clauses it re-drives
// here are the ones whose entry points it exercises.
//
// The page sentences each subtest implements:
//
//	docs/SPEC-TABLES.md §3.4          the size bounds, the flag and the leaf cap
//	docs/SPEC-TABLES.md §3.4          "THE LAYOUT IS A COMPILE-TIME CONSTANT OF
//	                                   THE UNIT. Every byte is settled by the
//	                                   compiler, so a backend emits it as a
//	                                   constant byte array and its length, and the
//	                                   hash as a constant"
//	docs/SPEC-TABLES.md §3.4          the generator bound: "THE IDENTITY PLAN IS
//	                                   BUILT BY THE SCHEMA COMPILER, once, and
//	                                   every backend lays the finished array down
//	                                   as static data — one answer for every port"
//	docs/FIXED-FORM-ALGORITHM.md §4.2 "The schema compiler does the walk once and
//	                                   every backend lays the finished array down
//	                                   as static data — one answer for every port"
//	docs/FIXED-FORM-ALGORITHM.md §5.8 row 15 and §5.9 #20  the shared
//	                                   generator-side COMPILE this page still
//	                                   names as the TOOLCHAIN's debt
package compiler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSharedRoadmapCompile drives the two shared-gate obligations of the NEW
// Fixed Tables work set through the production entry points (Compiler.Load for
// S5; Compiler.Generate for S6).  Each clause quotes the sentence it implements.
func TestSharedRoadmapCompile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id  string
		run func(*testing.T)
	}{
		{id: "shared/S5", run: testSharedS5},
		{id: "shared/S6", run: testSharedS6},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// testSharedS5 asserts shared/S5: "B1–B4, the compiler-side bounds: the 4096
// warning, the 65536 declared refusal, --fixed-record-limit, the leaf cap. Go
// only, by design" (docs/roadmap.sexp shared/S5; docs/SPEC-TABLES.md §3.4).  One
// subtest per bound, each driving the production entry point.  The bounds are
// also held, one test each, by
// TestFixedRecordPastTheAdvisoryBoundWarns (B1),
// TestFixedRecordPastTheWireCeilingLosesTheForm (B2),
// TestFixedRecordLimitRefusesTheCompile (B3) and
// TestWalkedElementArrayPastTheCapIsRefusedByName (B4); this gate is the index
// that proves each sentence of §3.4 through the same calls.
func testSharedS5(t *testing.T) {
	t.Run("B1_4096_warning", func(t *testing.T) {
		// "4096 BYTES OF RECORD BODY: A WARNING, ALWAYS ON ... the compiler
		// names the table and the size ... Nothing about the wire changes at
		// 4096" (docs/SPEC-TABLES.md §3.4).  The table still carries the form.
		dir, body := fixedSizeUnit(t, "Chunky", ir.TableFixedRecordWarnBytes)
		warns, err := loadWithWarnings(t, dir, 0)
		if err != nil {
			t.Fatalf("4096 is advisory and must not fail a compile: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("exactly one advisory, got %d: %v", len(warns), warns)
		}
		for _, want := range []string{"Chunky", fmt.Sprint(body), fmt.Sprint(ir.TableFixedRecordWarnBytes), "§3.4"} {
			if !strings.Contains(warns[0], want) {
				t.Errorf("the advisory must carry %q — a warning that names neither the table nor the size is one nobody can act on: %s", want, warns[0])
			}
		}
		if !strings.Contains(generatedFixedForm(t, dir), "ChunkyFixedLayout") {
			t.Error("a table past 4096 must still carry the fixed form: nothing about the wire changes at the advisory bound")
		}
	})

	t.Run("B2_65536_declared_refusal", func(t *testing.T) {
		// "65536 BYTES OF RECORD BODY: THE FORM IS NOT EMITTED, AND THE TABLE IS
		// NAMED ... A fixed table past it therefore KEEPS FORM 1 ... and the
		// compiler names it and its size rather than dropping the form in
		// silence" (docs/SPEC-TABLES.md §3.4).  Not a compile refusal.
		dir, body := fixedSizeUnit(t, "Huge", ir.TableFixedRecordMaxBytes)
		warns, err := loadWithWarnings(t, dir, 0)
		if err != nil {
			t.Fatalf("the ceiling refuses the FORM and never the unit: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("exactly one report, got %d: %v", len(warns), warns)
		}
		for _, want := range []string{"Huge", fmt.Sprint(body), fmt.Sprint(ir.TableFixedRecordMaxBytes), "form 1"} {
			if !strings.Contains(warns[0], want) {
				t.Errorf("the ceiling report must carry %q: %s", want, warns[0])
			}
		}
		if got := generatedFixedForm(t, dir); strings.Contains(got, "HugeFixedLayout") {
			t.Error("a table past the WIRE CEILING must not carry the fixed form: a reader holds an untrusted peer to the same 65536 and would refuse the record it writes")
		}
	})

	t.Run("B3_fixed_record_limit", func(t *testing.T) {
		// "--fixed-record-limit N MAKES THE ADVICE A GATE, AND ONLY EVER LOWERS
		// ... set it and a fixed table whose record body exceeds N bytes DOES
		// NOT COMPILE" (docs/SPEC-TABLES.md §3.4).
		dir, body := fixedSizeUnit(t, "Chunky", ir.TableFixedRecordWarnBytes)
		if _, err := loadWithWarnings(t, dir, 0); err != nil {
			t.Fatalf("the flag is off by default: %v", err)
		}
		_, err := loadWithWarnings(t, dir, ir.TableFixedRecordWarnBytes)
		if err == nil {
			t.Fatal("--fixed-record-limit must FAIL the compile: a gate that only warns is the advisory again")
		}
		diags, ok := errors.AsType[Diagnostics](err)
		if !ok || len(diags) != 1 {
			t.Fatalf("one diagnostic per table, got %v", err)
		}
		for _, want := range []string{"Chunky", fmt.Sprint(body), "fixed-record-limit", "§3.4"} {
			if !strings.Contains(diags[0].Error(), want) {
				t.Errorf("the refusal must carry %q: %s", want, diags[0])
			}
		}
		if _, err := loadWithWarnings(t, dir, ir.TableFixedRecordMaxBytes); err != nil {
			t.Fatalf("a limit the table is inside must not refuse it: %v", err)
		}
	})

	t.Run("B4_leaf_cap", func(t *testing.T) {
		// "A GENERATOR BOUND ... The walk is bounded, so a type whose leaves do
		// not fit one plan does not carry the form ... AN ARRAY OF A FLAT TYPE
		// IS ONE LEAF ... the bound is reached only by a large array of a type
		// carrying text, a count, a union or an optional" (docs/SPEC-TABLES.md
		// §3.4).  A DECLARED fixed table past the cap is a refusal by name.
		src := fmt.Sprintf("package probe\n\ntype Cell\n{\n    label string(4)\n}\n\nfixed table Wide\n{\n    cells [..%d]Cell\n}\n", ir.TableFixedLeafCap)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		paths, err := GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		var warns []string
		c := New()
		c.OnWarn = func(msg string) { warns = append(warns, msg) }
		if _, err := c.Load(paths); err == nil {
			t.Fatal("a DECLARED fixed table past the leaf cap must not compile")
		} else {
			for _, want := range []string{"Wide", fmt.Sprint(ir.TableFixedLeafCap), "leaves", "§3.4", "DECLARED"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal must carry %q: %s", want, err)
				}
			}
		}
		for _, w := range warns {
			if strings.Contains(w, "leaf cap") || strings.Contains(w, "leaves") {
				t.Errorf("the cap refused and warned as well: %s", w)
			}
		}
		// THE FLAT-ELEMENT FOLD, the control: an array of a scalar is ONE leaf
		// however long it is, so the cap must say nothing about it.
		flat := leafCapUnit(t, "package probe\n\nfixed table Flat\n{\n    marks [..8192]int32\n}\n")
		if fw, fe := ir.TableFixedLeafCapRefusals(flat); len(fw) != 0 || len(fe) != 0 {
			t.Fatalf("an array of a FLAT element is the count and one run; the cap must not reach it: warns=%v errs=%v", fw, fe)
		}
	})
}

// testSharedS6 asserts shared/S6, the plan laid down as data: "THE LAYOUT IS A
// COMPILE-TIME CONSTANT OF THE UNIT" and "THE IDENTITY PLAN IS BUILT BY THE
// SCHEMA COMPILER, once, and every backend lays the finished array down as
// static data — one answer for every port" (docs/SPEC-TABLES.md §3.4;
// docs/FIXED-FORM-ALGORITHM.md §4.2).  For one unit the test computes the
// layout once, off the shared walk, and reads the SAME bytes back out of each
// of the nine emitters' source, so the one COMPILE's answer reaches every leg.
//
// THE BOUNDARY, NAMED: this is the IDENTITY plan's data (the unit's own layout).
// §5.8 row 15 and §5.9 #20 owe the same for the COMPILED (non-identity) plans,
// which today each leg still builds from the lock's bytes at first use; that is
// a different obligation from the one asserted here and is not claimed by it.
func testSharedS6(t *testing.T) {
	dir := t.TempDir()
	const schema = "package probe\n\nfixed table Row\n{\n    x int32\n    name string(8)\n    marks [4]int32\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "Unit.schema"), []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Row"]
	if st == nil {
		t.Fatal("the fixture declares fixed table Row")
	}

	// THE ONE COMPILE: the layout bytes the schema compiler settles for this
	// unit, and the hash that binds them (docs/FIXED-FORM-ALGORITHM.md §5.2,
	// off ir.TableFixedWalkRoot and ir.TableFixedLayoutBytes).
	want := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	wantHash := ir.TableFixedLayoutHash(want, st)
	if len(want) == 0 {
		t.Fatal("the shared walk settles a non-empty layout")
	}

	for _, target := range fixedLineageShipTargets {
		target := target
		t.Run(target, func(t *testing.T) {
			files, err := c.Generate(u, target, nil)
			if err != nil {
				t.Fatalf("%s: generate: %v", target, err)
			}
			var body strings.Builder
			for _, data := range files {
				body.Write(data)
			}
			src := body.String()
			// THE SAME PLAN BYTES: the layout the emitter laid down is the one
			// the shared compile settled, byte for byte.
			got, err := sharedPlanLayoutBytes(target, src)
			if err != nil {
				t.Fatalf("%s: %v", target, err)
			}
			if !bytesEqual(got, want) {
				t.Errorf("%s: the emitter laid down %d layout bytes that are not the shared compile's %d — every backend lays THE FINISHED ARRAY down (docs/SPEC-TABLES.md §3.4)", target, len(got), len(want))
			}
			// AND THE HASH IS THE SAME COMPILE'S: the leg's spelling of the
			// one hash the schema compiler computed.
			if !strings.Contains(src, fixedLineageHashSpelling(target, wantHash)) {
				t.Errorf("%s: the emitted source carries no constant for the shared layout hash 0x%016x — the plan reached the emitter but not its static data (§5.2)", target, wantHash)
			}
		})
	}
}

// sharedPlanLayoutBytes reads the emitted layout byte array back out of one
// target's source.  The layout is the ONE byte sequence every leg shares
// (docs/SPEC-TABLES.md §3.4), so each leg spells the same constant; the
// markers below are where each leg writes it, and each is checked for length
// against the shared compile's own walk by the caller.
func sharedPlanLayoutBytes(target, src string) ([]byte, error) {
	var region string
	switch target {
	case "c":
		region = between(src, "row_fixed_layout[] = {", "}")
	case "cpp":
		region = between(src, "RowFixedLayout[] = {", "}")
	case "cs":
		region = between(src, "RowFixedLayout = new byte[] {", "}")
	case "dart":
		region = between(src, "rowFixedLayout = Uint8List.fromList(const <int>[", "]")
	case "go":
		region = between(src, "RowFixedLayout = []byte{", "}")
	case "js":
		region = between(src, "RowFixedLayout = new Uint8Array([", "]")
	case "rust":
		region = between(src, "ROW_FIXED_BLOCK: [u8;", "];")
	case "elixir":
		region = between(src, "@row_layout \"", "\"")
		return decodeEscapedHex(region), nil
	case "java":
		region = between(src, "byte[] layout = {", "};")
		return decodeDecimalBytes(region), nil
	default:
		return nil, fmt.Errorf("no layout spelling for target %q", target)
	}
	if region == "" {
		return nil, fmt.Errorf("the emitted source has no layout constant for this leg")
	}
	return decodeHexBytes(region), nil
}

// between is the text after the first start marker up to the first end marker
// after it, stripped of line comments so a per-field `//` note beside a byte
// cannot smuggle a number into the parse.
func between(src, start, end string) string {
	i := strings.Index(src, start)
	if i < 0 {
		return ""
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return ""
	}
	region := rest[:j]
	var clean strings.Builder
	for _, line := range strings.Split(region, "\n") {
		if k := strings.Index(line, "//"); k >= 0 {
			line = line[:k]
		}
		clean.WriteString(line)
		clean.WriteByte('\n')
	}
	return clean.String()
}

var sharedPlanHex = regexp.MustCompile(`0x([0-9a-fA-F]{2})`)

// decodeHexBytes reads every two-digit hex literal in a region, in order.
func decodeHexBytes(region string) []byte {
	var out []byte
	for _, m := range sharedPlanHex.FindAllStringSubmatch(region, -1) {
		v, _ := strconv.ParseUint(m[1], 16, 8)
		out = append(out, byte(v))
	}
	return out
}

var sharedPlanEscapedHex = regexp.MustCompile(`\\x([0-9A-Fa-f]{2})`)

// decodeEscapedHex reads every `\xNN` escape an Elixir binary literal carries.
func decodeEscapedHex(region string) []byte {
	var out []byte
	for _, m := range sharedPlanEscapedHex.FindAllStringSubmatch(region, -1) {
		v, _ := strconv.ParseUint(m[1], 16, 8)
		out = append(out, byte(v))
	}
	return out
}

var sharedPlanDecimal = regexp.MustCompile(`-?\d+`)

// decodeDecimalBytes reads every decimal byte literal a Java array carries.
// Java has no unsigned byte, so a wire byte past 127 is written as its signed
// value; the low eight bits are the byte.
func decodeDecimalBytes(region string) []byte {
	var out []byte
	for _, tok := range sharedPlanDecimal.FindAllString(region, -1) {
		v, err := strconv.ParseInt(tok, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, byte(v))
	}
	return out
}

// bytesEqual is a tiny equality so the failure message above can report both
// lengths without importing bytes for one call.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
