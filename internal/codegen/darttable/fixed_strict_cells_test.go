package darttable

// The Dart fixed cells' STRICT read (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6,
// §5.3; docs/SPEC-TABLES.md §4, §6.5): one read is the whole value or a
// refusal, never a clamp that lands a partially-repaired value beside a
// counter. These tests pin the generated artifact — the fixed load, the
// per-type decode and the plan run — to that contract for the cells the
// 2026-11-08 STRICT-100 audit returned to :todo (the valid-data rows,
// F1-F3, R16-R18, R24, R27, C3-C5, C12, C13, E9, P3, P4), with a negative
// control that plants each fault in the real generated artifact.
//
// This is a unit test: it takes no real time, sockets or network, and does
// not run the real Dart interpreter. It inspects the generated artifact
// directly; `make tables-dart-fixed-form` is the functional harness.

import (
	"sort"
	"strings"
	"testing"
)

const dartFixedCellsSchema = `package probe

enum E { a, b }

type Inner { x int32 = 1 | min = 0, max = 100 }

union U { inner Inner }

fixed table Leaf {
    flag bool
    label string(3)
}

fixed table R {
  lead uint32
  flag bool
  flags [..4]bool
  present ?int32
  e E
  rng int32 | min = -10, max = 10
  ratio float32 | min = 0, max = 1, resolution = 0.01
  arr [..4]int32
  fixedArr [2]int32
  s string(8)
  w wstring(4)
  b bytes(6)
  leaf Leaf
  opt ?Leaf
  u U
  keyed [E]int32
  big int64
  u128 uint128
}
`

func dartFixedCellsArtifact(t *testing.T) (decode, run, load, all string) {
	t.Helper()
	u := unitFrom(t, dartFixedCellsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.Write(files[name])
		b.WriteString("\n")
	}
	all = b.String()
	decode = dartFn(all, "void rFixedDecode(")
	run = dartFn(all, "int tableFixedRun(")
	load = dartFn(all, "int rFixedLoad(")
	if decode == "" || run == "" || load == "" {
		t.Fatalf("generated artifact is missing a fixed surface: decode=%t run=%t load=%t", decode != "", run != "", load != "")
	}
	return decode, run, load, all
}

// checkDartFixedCellsStrict reports every clause of the strict read that the
// generated artifact does not hold.
func checkDartFixedCellsStrict(decode, run, load, all string) []string {
	var faults []string
	add := func(ok bool, fault string) {
		if !ok {
			faults = append(faults, fault)
		}
	}
	// C12, C13, R17: a bool or present byte other than 0/1 refuses
	// wire_damaged (TableFixedRefusal.damaged in Dart's spelling).
	add(strings.Contains(decode, "final b = view.getUint8(at + 4);"), "decode lacks bool byte read")
	add(strings.Contains(decode, "if (b > 1) {"), "decode lacks wire_damaged bool refusal")
	add(strings.Contains(decode, "final p = view.getUint8(at +"), "decode lacks present byte read")
	add(strings.Contains(decode, "if (p > 1) {"), "decode lacks wire_damaged present refusal")
	add(strings.Count(decode, "report.refused = TableFixedRefusal.damaged;") >= 3, "decode lacks wire_damaged refusals")
	// C3-C5: a text length past its cap refuses length_over_bound, in the
	// decode and in the plan run; ill-formed used text refuses by name.
	for _, want := range []string{"TableFixedRefusal.lengthOverBound"} {
		add(strings.Contains(decode, want), "decode lacks "+want)
		add(strings.Contains(run, want), "runtime lacks "+want)
	}
	add(strings.Contains(run, "TableFixedRefusal.textIllFormed"), "runtime lacks TableFixedRefusal.textIllFormed")
	// Counts, ranges, ordinals and tags past their sets refuse, never clamp.
	add(strings.Contains(run, "TableFixedRefusal.countOverBound"), "runtime lacks TableFixedRefusal.countOverBound")
	add(strings.Contains(run, "TableFixedRefusal.outOfRange"), "runtime lacks TableFixedRefusal.outOfRange")
	add(strings.Contains(decode, "TableFixedRefusal.outOfRange"), "decode lacks TableFixedRefusal.outOfRange")
	add(strings.Contains(decode, "report.refuseNonFiniteFloat('R.ratio')"), "decode lacks non-finite float refusal")
	// F1-F3: the form byte names its direction before the file's length.
	add(strings.Contains(load, "TableFixedRefusal.previousForm"), "load lacks previous_form refusal")
	add(strings.Contains(load, "TableFixedRefusal.messageFormAsFile"), "load lacks message_form_as_file refusal")
	add(strings.Contains(load, "TableFixedRefusal.newerForm"), "load lacks newer_form refusal")
	// Framing and lineage refusals, each by name, each returning -1.
	for _, want := range []string{
		"TableFixedRefusal.damaged",
		"TableFixedRefusal.layoutDamaged",
		"TableFixedRefusal.layoutNewer",
		"TableFixedRefusal.layoutUnsupported",
		"TableFixedRefusal.noLayout",
		"TableFixedRefusal.planTooLarge",
		"TableFixedRefusal.batchTooLarge",
		"TableFixedRefusal.nonFiniteFloat",
	} {
		add(strings.Contains(load, want), "load lacks "+want)
	}
	add(strings.Contains(load, "report.layoutHash = hash;"), "load does not report the file's layout hash")
	add(strings.Contains(load, "rest % recordBytes != 0"), "load lacks ragged-tail refusal")
	// The one read is total: on a refusal every record written so far goes
	// back to its defaults and the load returns -1; the census lands once,
	// only on a read that returns (R16).
	add(strings.Contains(load, "if (report.refused != TableFixedRefusal.none) {"), "load lacks refused-discard")
	add(strings.Contains(load, "initR(values[j])"), "load does not restore defaults on refusal")
	add(strings.Contains(load, "report.unknown += censusUnknown;"), "load census is not landed once on success")
	add(strings.Contains(load, "return n;"), "load lacks its one successful return")
	add(strings.Contains(run, "report.widened++;"), "runtime lacks widened counter")
	// E9 and the retired report: no duplicate, clamped, malformed or
	// kind-mismatch counter may ride the fixed report.
	for _, banned := range []string{
		"report.clamped", "clamped++", "report.oor", "oor++",
		"report.malformed", "report.kindMismatch", "report.duplicate",
		"TableFixedRefusal.layoutMalformed",
		"int clamped =", "int malformed =", "int kindMismatch =", "int duplicate =",
	} {
		add(!strings.Contains(all, banned), "generated code still carries retired name "+banned)
	}
	add(strings.Contains(all, "int refused = TableFixedRefusal.none;"), "fixed report lacks refused")
	add(strings.Contains(all, "int unknown = 0;"), "fixed report lacks unknown")
	add(strings.Contains(all, "int widened = 0;"), "fixed report lacks widened")
	// The valid-data rows ride the decode: every declared kind is projected.
	for _, want := range []string{
		"view.getUint32(at, Endian.little)",               // uint32
		"view.getInt64(at +",                              // int64
		"UInt128(",                                        // uint128
		"value.sLength", "value.wLength", "value.bLength", // string, wstring, bytes
		"leafFixedDecode(value.leaf",     // nested fixed table
		"innerFixedDecode(value.u.inner", // union arm
	} {
		add(strings.Contains(decode, want), "decode lacks valid-data projection "+want)
	}
	return faults
}

func TestDartFixedCellsStrict(t *testing.T) {
	t.Parallel()
	decode, run, load, all := dartFixedCellsArtifact(t)
	if faults := checkDartFixedCellsStrict(decode, run, load, all); len(faults) != 0 {
		t.Errorf("the strict read does not hold:\n  %s", strings.Join(faults, "\n  "))
	}
}

// TestDartFixedCellsStrictNegativeControl plants each fault in a copy of the
// real generated artifact and requires the same checker to go red on the
// specific fault. It is the guard that TestDartFixedCellsStrict would not go
// green over code that still clamps, normalizes or counts a retired name.
func TestDartFixedCellsStrictNegativeControl(t *testing.T) {
	t.Parallel()
	decode, run, load, all := dartFixedCellsArtifact(t)
	if faults := checkDartFixedCellsStrict(decode, run, load, all); len(faults) != 0 {
		t.Fatalf("clean artifact should hold the strict read:\n  %s", strings.Join(faults, "\n  "))
	}
	hasFault := func(faults []string, want string) bool {
		for _, f := range faults {
			if f == want {
				return true
			}
		}
		return false
	}
	mustChange := func(name, mutated, original string) {
		t.Helper()
		if mutated == original {
			t.Fatalf("negative control %s did not change the real artifact", name)
		}
	}

	// A bool byte of 2 landing true again.
	bad := strings.ReplaceAll(decode, "if (b > 1) {", "if (b > 255) {")
	mustChange("bool", bad, decode)
	if faults := checkDartFixedCellsStrict(bad, run, load, all); !hasFault(faults, "decode lacks wire_damaged bool refusal") {
		t.Fatalf("negative control did not flag the bool landing: %v", faults)
	}
	// A present byte of 2 landing present again.
	bad = strings.ReplaceAll(decode, "if (p > 1) {", "if (p > 255) {")
	mustChange("present", bad, decode)
	if faults := checkDartFixedCellsStrict(bad, run, load, all); !hasFault(faults, "decode lacks wire_damaged present refusal") {
		t.Fatalf("negative control did not flag the present landing: %v", faults)
	}
	// A count past its bound clamped in the run instead of refused.
	badRun := strings.Replace(run, "return TableFixedRefusal.countOverBound;", "counted = size;", 1)
	mustChange("count", badRun, run)
	if faults := checkDartFixedCellsStrict(decode, badRun, load, all); !hasFault(faults, "runtime lacks TableFixedRefusal.countOverBound") {
		t.Fatalf("negative control did not flag the count clamp: %v", faults)
	}
	// A retired clamped counter planted in the real artifact text.
	badAll := strings.Replace(all, "int widened = 0;", "int widened = 0;\n  int clamped = 0;", 1)
	mustChange("retired", badAll, all)
	if faults := checkDartFixedCellsStrict(decode, run, load, badAll); !hasFault(faults, "generated code still carries retired name int clamped =") {
		t.Fatalf("negative control did not flag the retired counter: %v", faults)
	}
	// A load that lands a refused read: the discard of written records gone.
	badLoad := strings.ReplaceAll(load, "initR(values[j])", "values[j]")
	mustChange("discard", badLoad, load)
	if faults := checkDartFixedCellsStrict(decode, run, badLoad, all); !hasFault(faults, "load does not restore defaults on refusal") {
		t.Fatalf("negative control did not flag the landing load: %v", faults)
	}
}
