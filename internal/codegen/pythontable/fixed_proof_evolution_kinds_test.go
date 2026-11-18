package pythontable_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// pythonFixedKindDecl is one numeric leaf of the fixed form's closed kind set:
// its declaration, the raw value the older C++ writer stores, and its place on
// the docs/FIXED-FORM-ALGORITHM.md §1 ladders (family, then rung).
type pythonFixedKindDecl struct {
	name, decl, setup string
	family            string
	rung              int
}

// pythonFixedKindDecls is every numeric kind a fixed field can be declared
// at: i8..i64 and i128, u8..u128, f32 and f64, and the signed and unsigned
// fixed-point kinds at all five storage widths. Each value is the one a
// widening gets wrong: the signed minimum (sign extension), the unsigned
// maximum (no sign extension), a fixed-point raw with its top bit set.
var pythonFixedKindDecls = []pythonFixedKindDecl{
	{"i8", "int8", "-128", "int", 0},
	{"i16", "int16", "-32768", "int", 1},
	{"i32", "int32", "-2147483647 - 1", "int", 2},
	{"i64", "int64", "-9223372036854775807ll - 1", "int", 3},
	{"i128", "int128 | min = -100, max = 100", "-5", "int128", 0},
	{"u8", "uint8", "255", "uint", 0},
	{"u16", "uint16", "65535", "uint", 1},
	{"u32", "uint32", "0xffffffffu", "uint", 2},
	{"u64", "uint64", "0xffffffffffffffffull", "uint", 3},
	{"u128", "uint128", "~(serialize::uint128_t)0", "uint128", 0},
	{"f32", "float32", "-1234.5625f", "float", 0},
	{"f64", "float64", "-1234.5625", "float", 1},
	{"x8", "fixed(4,4) | min = -8, max = 7", "-128", "fixed", 0},
	{"x16", "fixed(12,4) | min = -8, max = 7", "-128", "fixed", 1},
	{"x32", "fixed(28,4) | min = -8, max = 7", "-128", "fixed", 2},
	{"x64", "fixed(60,4) | min = -8, max = 7", "-128", "fixed", 3},
	{"x128", "fixed(124,4) | min = -8, max = 7", "-128", "fixed", 4},
	{"q8", "ufixed(4,4) | min = 0, max = 15", "240", "ufixed", 0},
	{"q16", "ufixed(12,4) | min = 0, max = 15", "240", "ufixed", 1},
	{"q32", "ufixed(28,4) | min = 0, max = 15", "240", "ufixed", 2},
	{"q64", "ufixed(60,4) | min = 0, max = 15", "240", "ufixed", 3},
	{"q128", "ufixed(124,4) | min = 0, max = 15", "240", "ufixed", 4},
}

// pythonFixedLadderWidens is the fixed form's ladder as the algorithm states
// it (§1: "2..5, 6..9, 20..24, 25..29, and 10 -> 11", inside a family and
// upward only). The 128-bit integers, kinds 18 and 19, are on no rung: an
// int64 into an int128 is a kind that moved.
func pythonFixedLadderWidens(from, to pythonFixedKindDecl) bool {
	return from.family == to.family && to.rung > from.rung
}

func pythonFixedKindSchema(d pythonFixedKindDecl) string {
	return "package probe\nfixed table T {\n lead uint32 = 1\n v " + d.decl + "\n trail uint32 = 2\n}\n"
}

// pythonFixedKindVerdict is one Python read of one older file: the same
// fields CppVerdict carries, from the generated package's own report.
type pythonFixedKindVerdict struct {
	refused bool
	reason  string
	count   int64
	widened int
	saved   []byte
}

func (v pythonFixedKindVerdict) String() string {
	if v.refused {
		return "refused " + v.reason
	}
	return fmt.Sprintf("ok n=%d widened=%d", v.count, v.widened)
}

// pythonFixedKindAgree holds one Python verdict to the C++ reference's: the
// same accept or refuse, and on accept the same record count, the same
// widened count and the same saved bytes.
func pythonFixedKindAgree(py pythonFixedKindVerdict, cpp fixedproof.CppVerdict) error {
	if py.refused != cpp.Refused {
		return fmt.Errorf("python %s, C++ %s", py, cpp)
	}
	if py.refused {
		return nil
	}
	if py.count != cpp.Count || py.widened != cpp.Widened {
		return fmt.Errorf("python %s, C++ %s", py, cpp)
	}
	if !bytes.Equal(py.saved, cpp.Saved) {
		return fmt.Errorf("saved bytes differ: python %x, C++ %x", py.saved, cpp.Saved)
	}
	return nil
}

const pythonFixedKindScript = `from probe import T, LoadReport, Refused, InvalidValue
with open('names') as f:
    names = f.read().split()
for name in names:
    with open(name, 'rb') as f:
        wire = f.read()
    report = LoadReport()
    try:
        rows = T.fixed_load(wire, report=report)
    except Refused as e:
        print(name, 'refused', e.reason)
        continue
    try:
        saved = T.fixed_save(rows)
    except InvalidValue:
        saved = b''
    with open('saved_' + name, 'wb') as f:
        f.write(saved)
    print(name, 'ok', len(rows), report.widened)
`

// pythonFixedKindRun reads every named file with the generated package under
// both interpreter pins, requires the pins to agree, and returns the verdicts.
func pythonFixedKindRun(t *testing.T, files map[string][]byte, inputs map[string][]byte) map[string]pythonFixedKindVerdict {
	t.Helper()
	var first map[string]pythonFixedKindVerdict
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		in := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + pythonFixedKindScript)}
		maps.Copy(in, inputs)
		names := make([]string, 0, len(inputs))
		for name := range inputs {
			names = append(names, name)
		}
		in["names"] = []byte(strings.Join(names, "\n"))
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", in)
		if err != nil {
			t.Fatalf("kind-pair probe: %v\n%s\n%s", err, out, stderr)
		}
		got := map[string]pythonFixedKindVerdict{}
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			f := strings.Fields(line)
			switch {
			case len(f) == 3 && f[1] == "refused":
				got[f[0]] = pythonFixedKindVerdict{refused: true, reason: f[2], count: -1}
			case len(f) == 4 && f[1] == "ok":
				n, _ := strconv.ParseInt(f[2], 10, 64)
				w, _ := strconv.Atoi(f[3])
				saved, err := os.ReadFile(filepath.Join(dir, "saved_"+f[0]))
				if err != nil {
					t.Fatal(err)
				}
				got[f[0]] = pythonFixedKindVerdict{count: n, widened: w, saved: saved}
			default:
				t.Fatalf("kind-pair probe line %q", line)
			}
		}
		if len(got) != len(inputs) {
			t.Fatalf("kind-pair probe answered %d of %d files", len(got), len(inputs))
		}
		if first == nil {
			first = got
			continue
		}
		for name, v := range got {
			if w := first[name]; v.String() != w.String() || !bytes.Equal(v.saved, w.saved) {
				t.Fatalf("%s: the interpreter pins disagree: %s and %s", name, w, v)
			}
		}
	}
	return first
}

// TestPythonFixedProofEvolutionKindPairs reads every numeric kind's older
// file with a reader of every other numeric kind, the whole ordered matrix of
// the fixed form's closed numeric kind set, in Python and in the live C++
// reference built with the same lineage. Each pair must get the same verdict
// from both, the accepted pairs must be exactly the algorithm's ladder (every
// lawful widening, adjacent or not), and an accepted pair must land the same
// count, the same widened count and the same saved bytes; every other pair
// (narrowing, signedness, family, the 128-bit integer kinds) must refuse in
// both. Its negative control is TestPythonFixedProofEvolutionKindPairsNegativeControl.
func TestPythonFixedProofEvolutionKindPairs(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the live C++ reference")
	type writer struct {
		entry fixedproof.LineageEntry
		wire  []byte
	}
	writers := make([]writer, len(pythonFixedKindDecls))
	t.Run("writers", func(t *testing.T) {
		for i, d := range pythonFixedKindDecls {
			t.Run(d.name, func(t *testing.T) {
				t.Parallel()
				u := fixedproof.ParseCheckUnit(t, pythonFixedKindSchema(d))
				_, _, body := fixedproof.RootFacts(u.Tables["T"])
				setup := "value[0].lead = 0xAAAAAAAAu; value[0].v = " + d.setup + "; value[0].trail = 0xBBBBBBBBu;"
				wire, _ := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "kind-" + d.name, Root: "T", Body: body, Count: 1, CppSetup: setup}, u)
				entry, ok := fixedproof.LineageEntryOf(u, "T")
				if !ok {
					t.Fatal("missing writer layout")
				}
				writers[i] = writer{entry: entry, wire: wire}
			})
		}
	})
	if t.Failed() {
		return
	}
	t.Run("readers", func(t *testing.T) {
		for j, to := range pythonFixedKindDecls {
			t.Run("to="+to.name, func(t *testing.T) {
				t.Parallel()
				newer := fixedproof.ParseCheckUnit(t, pythonFixedKindSchema(to))
				var older []fixedproof.LineageEntry
				inputs := map[string][]byte{}
				for i, from := range pythonFixedKindDecls {
					if i == j {
						continue
					}
					older = append(older, writers[i].entry)
					inputs[from.name] = writers[i].wire
				}
				cpp := fixedproof.CppLineageReader(t, "kind-to-"+to.name, newer, "T", older, 1)
				files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": pythonFixedLineage(map[string][]fixedproof.LineageEntry{"T": older})["T"]})
				if err != nil {
					t.Fatal(err)
				}
				py := pythonFixedKindRun(t, files, inputs)
				cppVerdicts := map[string]fixedproof.CppVerdict{}
				for i, from := range pythonFixedKindDecls {
					if i == j {
						continue
					}
					c := cpp(writers[i].wire)
					cppVerdicts[from.name] = c
					lawful := pythonFixedLadderWidens(from, to)
					if c.Refused == lawful {
						t.Errorf("%s -> %s: the C++ reference answered %s, the ladder says lawful=%v", from.name, to.name, c, lawful)
					}
					if lawful && to.rung == 4 {
						// A REFERENCE DEFECT, held open here rather than copied:
						// the C++ widen op lands at most eight bytes
						// (TableFixedStoreNative has no 16-byte case), so a
						// fixed(I,F) widened into 128-bit storage reads back
						// as zero while the algorithm's §4 widen is exact.
						// The value Python must land is the one the C++
						// reference's own writer saves for it at the reader's
						// declaration; the day the reader lands it too, this
						// exception goes red and is removed.
						_, _, body := fixedproof.RootFacts(newer.Tables["T"])
						setup := "value[0].lead = 0xAAAAAAAAu; value[0].v = " + from.setup + "; value[0].trail = 0xBBBBBBBBu;"
						canonical, _ := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "kind-" + from.name + "-as-" + to.name, Root: "T", Body: body, Count: 1, CppSetup: setup}, newer)
						if bytes.Equal(c.Saved, canonical) {
							t.Errorf("%s -> %s: the C++ reader now lands the widened value exactly; remove the 128-bit storage exception", from.name, to.name)
						}
						c.Saved = canonical
					}
					if err := pythonFixedKindAgree(py[from.name], c); err != nil {
						t.Errorf("%s -> %s: %v", from.name, to.name, err)
					}
					if lawful && c.Widened != 1 {
						t.Errorf("%s -> %s: a lawful widening counted widened=%d in C++", from.name, to.name, c.Widened)
					}
				}
			})
		}
	})
}

// TestPythonFixedProofEvolutionKindPairsNegativeControl is the kind-pair
// gate's negative control: on one lawful pair, int8 into int16 (a signed
// minimum, the value sign extension exists for), Python and the live C++
// lineage reader agree; then a changed older file, a changed emitted writer
// and a dropped sign extension in the emitted runtime must each break that
// agreement by name.
func TestPythonFixedProofEvolutionKindPairsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the live C++ reference")
	from, to := pythonFixedKindDecls[0], pythonFixedKindDecls[1]
	if from.name != "i8" || to.name != "i16" || !pythonFixedLadderWidens(from, to) {
		t.Fatal("kind-pair control pair moved")
	}
	old := fixedproof.ParseCheckUnit(t, pythonFixedKindSchema(from))
	newer := fixedproof.ParseCheckUnit(t, pythonFixedKindSchema(to))
	_, _, body := fixedproof.RootFacts(old.Tables["T"])
	wire, _ := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "kind-control-old", Root: "T", Body: body, Count: 1, CppSetup: "value[0].lead = 0xAAAAAAAAu; value[0].v = " + from.setup + "; value[0].trail = 0xBBBBBBBBu;"}, old)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer layout")
	}
	older := []fixedproof.LineageEntry{entry}
	cpp := fixedproof.CppLineageReader(t, "kind-control", newer, "T", older, 1)(wire)
	if cpp.Refused || cpp.Widened != 1 {
		t.Fatalf("the C++ reference answered %s for a lawful widening", cpp)
	}
	files, err := pythontable.GenerateLineage(newer, pythonFixedLineage(map[string][]fixedproof.LineageEntry{"T": older}))
	if err != nil {
		t.Fatal(err)
	}
	if err := pythonFixedKindAgree(pythonFixedKindRun(t, files, map[string][]byte{"old": wire})["old"], cpp); err != nil {
		t.Fatalf("the unchanged pair disagrees: %v", err)
	}
	// A changed older file: the high byte of v, the last byte before trail.
	moved := bytes.Clone(wire)
	moved[len(moved)-5] ^= 0x40
	if pythonFixedKindAgree(pythonFixedKindRun(t, files, map[string][]byte{"old": moved})["old"], cpp) == nil {
		t.Fatal("kind_pairs_file_control: a changed older file reads as the reference's")
	}
	path := "probe/_schema/fixedform.py"
	source := string(files[path])
	for _, m := range []struct{ name, anchor, changed string }{
		{"kind_pairs_writer_control", `records.append(root["prefix"] + image)`, "image[0] ^= 1\n        " + `records.append(root["prefix"] + image)`},
		{"kind_pairs_sign_control", `_integer(source, src, size, op["signed"])`, `_integer(source, src, size, False)`},
	} {
		if strings.Count(source, m.anchor) != 1 {
			t.Fatalf("%s: anchor count %d", m.name, strings.Count(source, m.anchor))
		}
		mutant := maps.Clone(files)
		mutant[path] = []byte(strings.Replace(source, m.anchor, m.changed, 1))
		if pythonFixedKindAgree(pythonFixedKindRun(t, mutant, map[string][]byte{"old": wire})["old"], cpp) == nil {
			t.Fatalf("%s: the planted fault still agrees with the C++ reference", m.name)
		}
	}
}
