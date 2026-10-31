package rusttable

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestRetainsAndWidensOnEveryRootRust holds the Rust leg's retention and
// widening over EVERY variable-class file root of the conformance roster that
// the Rust generator gives a codec (docs/SPEC-TABLES.md §3, §4, §6.6), not the
// Card root alone (cardsretain_test.go keeps that one). Three halves:
//
//   - THE ROSTER (a pure read of generator output): the roots whose unit
//     emits <root>_load_retain are exactly the ROOTS wire_fuzz_all.rs
//     dispatches, both ways, so a root the generator learns to carry and the
//     driver does not answer is a red here before it is an `absent` line in
//     make tables-rust-wire-fuzz-all.
//   - RETENTION ON EVERY ROOT (functional: rustc): every pinned seed of every
//     carried root, driven through the all-roots driver's plain and retaining
//     pairs, answers the Go oracle's report, retention counters and saved
//     bytes (internal/tablewire DecodeRegionRetain / EncodeRetain).
//   - WIDENING ON READ (functional: rustc): a wire written by a narrow
//     declaration is read by a wide one; the Rust leg counts the oracle's
//     `widened`, lands the oracle's values, and saves the oracle's bytes (§4).
//
// A test that compiles Rust is a FUNCTIONAL test: it skips by name when rustc
// is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name when it is
// set, which is how make tables-rust-wire-fuzz-all runs it.
func TestRetainsAndWidensOnEveryRootRust(t *testing.T) {
	t.Parallel()

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	roster := manifestRoster(t, repo)
	carried, units := carriedRoots(t, repo, roster)
	dispatched := dispatchedRoots(t, repo)

	// THE ROSTER, both ways
	for key := range carried {
		if !dispatched[key] {
			t.Errorf("%s: the Rust generator emits its retaining codec but wire_fuzz_all.rs does not dispatch it", key)
		}
	}
	for key := range dispatched {
		if !carried[key] {
			t.Errorf("%s: wire_fuzz_all.rs dispatches it but no roster unit's Rust output carries its retaining codec", key)
		}
	}
	if len(carried) == 0 {
		t.Fatal("no roster root is carried by the Rust generator")
	}
	if t.Failed() {
		return
	}

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the all-roots driver cannot be built: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	t.Run("RetainOnEveryRoot", func(t *testing.T) {
		bin := buildAllRootsDriver(t, repo, units)
		var seeds []rosterSeed
		planted := map[string]bool{}
		for _, s := range roster {
			if !carried[s.unit+"."+s.root] {
				continue
			}
			seeds = append(seeds, s)
			// AND ONE SEED A ROOT THAT CARRIES AN UNKNOWN FIELD, so retention is
			// exercised on every root and not only where the corpus happens
			// to pin an evolution pair (docs/SPEC-TABLES.md §6.6)
			if key := s.unit + "." + s.root; !planted[key] {
				planted[key] = true
				wire, err := os.ReadFile(filepath.Join(repo, s.wire))
				if err != nil {
					t.Fatal(err)
				}
				p := s
				p.name += "+unknown"
				p.data = plantUnknown(t, wire)
				seeds = append(seeds, p)
			}
		}
		// A forged pointer cycle is accepted on load and refused on save
		// (docs/SPEC-TABLES.md §3.1). The ordinary corpus contains only
		// acyclic writers, so exercise the pinned hostile vector explicitly.
		if carried["graphdemo.Scene"] {
			seeds = append(seeds, rosterSeed{
				name: "graph_self_cycle", unit: "graphdemo", root: "Scene",
				wire: "testdata/wire/tables/fuzz-vectors/pointer_self_cycle.bin",
			})
			shared, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/graph_shared.bin"))
			if err != nil {
				t.Fatal(err)
			}
			shared[29] = byte(ir.TableKindUnion)
			seeds = append(seeds, rosterSeed{name: "graph_union_kind_mutant", unit: "graphdemo", root: "Scene", data: shared})
		}
		if carried["listdemo.Mixed"] {
			seeds = append(seeds, repeatedUnionSeeds(t)...)
		}
		// A POINTER UNDER A FALSE GUARD IS NOT AN EDGE (docs/SPEC-TABLES.md
		// §3.1, test/tables/G1.schema): guard_true_both_edges with its
		// `at_rest` byte cleared still carries `resting` and its record, which
		// the reader places and the save, numbering included, must drop.
		if carried["tblg1.Guarded"] {
			both, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/guard_true_both_edges.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if both[3] != 1 {
				t.Fatalf("guard_true_both_edges no longer opens with at_rest = true: % x", both[:4])
			}
			both[3] = 0
			seeds = append(seeds, rosterSeed{name: "guard_false_resting_rides", unit: "tblg1", root: "Guarded", data: both})
		}
		for _, retain := range []bool{false, true} {
			driveSeeds(t, repo, bin, units, seeds, retain)
		}
	})
	t.Run("WideningOnRead", func(t *testing.T) {
		widenOnRead(t)
	})
}

type rosterSeed struct {
	name, unit, root, wire string
	// data is a seed made here rather than read from the corpus: the first
	// seed of a root with one UNKNOWN field planted in its root body
	data []byte
	// retainWant, when set, is the RETAINING report this seed must answer in
	// the driver's spelling, pinned here instead of read from the oracle;
	// the saved bytes are still the oracle's
	retainWant string
	// retainedAtLeast is how many fields the ORACLE must retain from this
	// seed, so a seed meant to exercise retention cannot quietly stop doing it
	retainedAtLeast int
}

// mixed91317 is the default retaining fuzzer's mutant random #91317 of
// list_mixed (listdemo.Mixed, run seed 24845619678). The `hits` list (an
// array of unions, docs/SPEC-TABLES.md §2.9) rides TWICE: the first
// occurrence's element 0 is a Point arm whose body keeps one unknown field
// and then runs past its `L` (damaged: the element reads None), and the
// second occurrence is a whole Point arm with its own unknown field, a damage
// arm and a None.
const mixed91317 = "01010e051e03020304050e12090203000000000000000400000000000000060e1a0f03070d0d080401001000091402000000000a04040700000000060e1a0f03070d0d080401000000090402000000000a040407000000000b0e0e040300000000320000006400000000e599f782764e0ad9ec2202864cfc63af522602864cfe63af052802864cff63afa82e867084edf24abbf00c9bccfb2d73b145c34435abfe73071702864cf5638f541502864cf463afc07fb38abe08637f68b7f0ae4c0cf65a0b00000000000000"

// repeatedUnionSeeds pins RETENTION ACROSS AN ARRAY OF UNIONS
// (docs/SPEC-TABLES.md §6.6): each element's arm body is its OWN body
// occurrence, and a later occurrence of the field discards every record an
// earlier one held, a damaged arm's included.
//
//   - mixed91317 itself: the second `hits` occurrence replaces the first
//     whole, so the record kept under the first occurrence's damaged arm goes
//     with it and nothing is lost: retained=4 retain_lost=0. The generated
//     Go leg (build/conformance-go wire-fuzz --retain) answers the same. The
//     Go oracle answers retain_lost=1 only because it keys an arm body by its
//     cell's address and the list's append moved the cell, so the discard
//     never finds it; that is the oracle's defect (internal/tablewire
//     decode.go, out of this leg's paths), and the count is pinned here.
//   - two Point arms in one list, each with its own unknown field: each
//     record rides back in ITS element's arm body, never both in element 0's
//     (a retain path that named the field and not the element did that).
func repeatedUnionSeeds(t *testing.T) []rosterSeed {
	t.Helper()
	mutant, err := hex.DecodeString(mixed91317)
	if err != nil {
		t.Fatal(err)
	}
	count := int(binary.LittleEndian.Uint64(mutant[len(mutant)-8:]))
	trailer := mutant[len(mutant)-8-8*count:]
	point := func(unknown, y byte) []byte {
		// arm ref 7 (Point), kind 13, L 13: unknown ref 8 (i32), ref 9 (i32), end
		return []byte{7, 13, 13, 8, 4, unknown, 0, 0, 0, 9, 4, y, 0, 0, 0, 0}
	}
	hits := []byte{byte(ir.TableKindUnion), 2}
	hits = append(hits, point(1, 2)...)
	hits = append(hits, point(5, 6)...)
	two := []byte{byte(ir.TableWireForm), 6, byte(ir.TableKindArray), byte(len(hits))}
	two = append(two, hits...)
	two = append(two, 0)
	two = append(two, trailer...)
	return []rosterSeed{
		{name: "mixed91317", unit: "listdemo", root: "Mixed", data: mutant,
			retainWant: "4,1,0,0,0,true,false retained=4 retain_lost=0", retainedAtLeast: 4},
		{name: "list_two_point_arms", unit: "listdemo", root: "Mixed", data: two, retainedAtLeast: 2},
	}
}

// plantUnknown writes one field this reader cannot name at the front of the
// root body (docs/SPEC-TABLES.md §3, §6.6): a string under a fresh id the
// trailer gains, which every reader skips as one `unknown` and a retaining
// reader keeps. References are trailer slots and node indices, never offsets,
// so nothing behind the planted field moves meaning.
func plantUnknown(t *testing.T, wire []byte) []byte {
	t.Helper()
	if len(wire) < 9 || wire[0] != byte(ir.TableWireForm) {
		t.Fatalf("a seed is not a variable-form file")
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	estart := len(wire) - 8 - 8*int(count)
	const planted = uint64(0x52455441494e0001) // "RETAIN" and a one
	for i := 0; i < int(count); i++ {
		if binary.LittleEndian.Uint64(wire[estart+8*i:]) == planted {
			t.Fatal("the planted id is already in the seed's trailer")
		}
	}
	var field []byte
	field = binary.AppendUvarint(field, count+1)
	field = append(field, byte(ir.TableKindString), 4, 'k', 'e', 'e', 'p')
	out := append([]byte{wire[0]}, field...)
	out = append(out, wire[1:estart]...)
	out = append(out, wire[estart:len(wire)-8]...)
	out = binary.LittleEndian.AppendUint64(out, planted)
	return binary.LittleEndian.AppendUint64(out, count+1)
}

// manifestRoster reads the default roster's FILE seeds from the conformance
// manifest, the instance and report rows wirefuzz.go reads (FORMAT.md).
func manifestRoster(t *testing.T, repo string) []rosterSeed {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []rosterSeed
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || (f[0] != "instance" && f[0] != "report") {
			continue
		}
		out = append(out, rosterSeed{name: f[1], unit: f[2], root: f[3], wire: f[4]})
	}
	if len(out) == 0 {
		t.Fatal("the manifest names no instance or report row")
	}
	return out
}

// carriedRoots generates every roster unit for rust and answers the roots whose
// output carries <root>_load_retain, with each carried unit's modules. A unit
// the generator refuses whole carries none.
func carriedRoots(t *testing.T, repo string, roster []rosterSeed) (map[string]bool, map[string]*carriedUnit) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "unit" {
			paths[f[1]] = f[2:]
		}
	}
	carried := map[string]bool{}
	units := map[string]*carriedUnit{}
	for _, s := range roster {
		cu, seen := units[s.unit]
		if !seen {
			p, ok := paths[s.unit]
			if !ok {
				t.Fatalf("the manifest names no unit %s", s.unit)
			}
			u := generateUnit(t, repo, p)
			modules, err := Generate(u)
			if err != nil {
				units[s.unit] = nil
				continue
			}
			cu = &carriedUnit{unit: u, modules: modules}
			units[s.unit] = cu
		}
		if cu == nil {
			continue
		}
		want := "pub fn " + ir.RustSnake(s.root) + "_load_retain("
		for _, body := range cu.modules {
			if bytes.Contains(body, []byte(want)) {
				carried[s.unit+"."+s.root] = true
				break
			}
		}
	}
	for name, cu := range units {
		if cu == nil {
			delete(units, name)
		}
	}
	return carried, units
}

type carriedUnit struct {
	unit    *ir.Unit
	modules map[string][]byte
}

var rootsRe = regexp.MustCompile(`\("([a-z0-9]+)", "([A-Za-z0-9]+)"\),`)

// dispatchedRoots parses ROOTS out of wire_fuzz_all.rs.
func dispatchedRoots(t *testing.T, repo string) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repo, "test/conformance/rust/src/wire_fuzz_all.rs"))
	if err != nil {
		t.Fatalf("the all-roots wire-fuzz driver is missing: %v", err)
	}
	start := bytes.Index(src, []byte("const ROOTS"))
	if start < 0 {
		t.Fatal("wire_fuzz_all.rs declares no ROOTS")
	}
	end := bytes.Index(src[start:], []byte("];"))
	out := map[string]bool{}
	for _, m := range rootsRe.FindAllSubmatch(src[start:start+end], -1) {
		out[string(m[1])+"."+string(m[2])] = true
	}
	return out
}

// layoutRustUnit writes one unit's wire modules as a module directory
// with a mod.rs, the layout test/conformance/rust/wire-fuzz-all-build writes.
func layoutRustUnit(t *testing.T, dir, name string, modules map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	var files []string
	for file := range modules {
		if file == BuildVersionModule+".rs" || file == VariableRuntimeModule+".rs" || file == TableEnumsModule+".rs" ||
			file == FixedRuntimeModule+".rs" || file == MessageRuntimeModule+".rs" ||
			strings.HasSuffix(file, "_records.rs") || strings.HasSuffix(file, "_fixed.rs") ||
			strings.HasSuffix(file, "_table.rs") || strings.HasSuffix(file, "_variable.rs") ||
			strings.HasSuffix(file, "_message.rs") {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	var mod strings.Builder
	for _, file := range files {
		body := bytes.ReplaceAll(modules[file], []byte("use crate::*;"), []byte("use super::*;"))
		if err := os.WriteFile(filepath.Join(dir, name, file), body, 0o600); err != nil {
			t.Fatal(err)
		}
		m := strings.TrimSuffix(file, ".rs")
		fmt.Fprintf(&mod, "#[allow(dead_code, unused_imports)]\npub mod %s;\n#[allow(unused_imports)]\npub use %s::*;\n", m, m)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "mod.rs"), []byte(mod.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rustcBuild(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "driver")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-A", "warnings", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated Rust did not build: %v\n%s", err, out)
	}
	return bin
}

func buildAllRootsDriver(t *testing.T, repo string, units map[string]*carriedUnit) string {
	t.Helper()
	dir := t.TempDir()
	for name, cu := range units {
		layoutRustUnit(t, dir, name, cu.modules)
	}
	main, err := os.ReadFile(filepath.Join(repo, "test/conformance/rust/src/wire_fuzz_all.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), main, 0o600); err != nil {
		t.Fatal(err)
	}
	return rustcBuild(t, dir)
}

// driveSeeds sends every seed, unmutated, through the driver's wire-fuzz
// protocol (wirefuzz.go: roster, then (index, size, wire) per seed) and holds
// each reply to the oracle (docs/SPEC-TABLES.md §4.2, §6.6).
func driveSeeds(t *testing.T, repo, bin string, units map[string]*carriedUnit, seeds []rosterSeed, retain bool) {
	t.Helper()
	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	stdout := bufio.NewReader(stdoutPipe)

	var roster bytes.Buffer
	_ = binary.Write(&roster, binary.LittleEndian, uint32(len(seeds)))
	for _, s := range seeds {
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(s.unit)))
		roster.WriteString(s.unit)
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(s.root)))
		roster.WriteString(s.root)
		roster.WriteByte(byte(ir.TableWireForm))
		if retain {
			roster.WriteByte(1)
		} else {
			roster.WriteByte(0)
		}
	}
	if _, err := stdin.Write(roster.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, s := range seeds {
		b, err := stdout.ReadByte()
		if err != nil {
			t.Fatalf("the driver did not answer the roster: %v\n%s", err, stderr.String())
		}
		if b == 0 {
			t.Errorf("%s.%s: the driver cannot open a root it dispatches", s.unit, s.root)
		}
	}
	if t.Failed() {
		return
	}
	for i, s := range seeds {
		wire := s.data
		if wire == nil {
			wire, err = os.ReadFile(filepath.Join(repo, s.wire))
			if err != nil {
				t.Fatal(err)
			}
		}
		var head [8]byte
		binary.LittleEndian.PutUint32(head[0:], uint32(i))
		binary.LittleEndian.PutUint32(head[4:], uint32(len(wire)))
		if _, err := stdin.Write(append(head[:], wire...)); err != nil {
			t.Fatal(err)
		}
		var reply [47]byte
		if _, err := io.ReadFull(stdout, reply[:]); err != nil {
			t.Fatalf("%s: the driver died: %v\n%s", s.name, err, stderr.String())
		}
		saveLen := int64(binary.LittleEndian.Uint64(reply[39:]))
		var saved []byte
		if saveLen >= 0 {
			saved = make([]byte, saveLen)
			if _, err := io.ReadFull(stdout, saved); err != nil {
				t.Fatalf("%s: the driver died: %v", s.name, err)
			}
		}
		leg := fmt.Sprintf("%d,%d,%d,%d,%d,%t,%t retained=%d retain_lost=%d",
			binary.LittleEndian.Uint32(reply[1:]), binary.LittleEndian.Uint32(reply[5:]), binary.LittleEndian.Uint32(reply[9:]),
			binary.LittleEndian.Uint32(reply[13:]), binary.LittleEndian.Uint32(reply[17:]), reply[21] != 0, reply[22] != 0,
			binary.LittleEndian.Uint32(reply[31:]), binary.LittleEndian.Uint32(reply[35:]))
		want, encoded := oracleSeed(t, units[s.unit].unit, s.root, wire, retain)
		if counts := strings.Split(strings.Fields(want)[0], ","); counts[5] == "true" {
			// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the
			// leg owes the refusal, and no counter, retention report or
			// saved byte is compared
			if reply[21] == 0 {
				t.Errorf("%s (%s.%s, retain=%t): the oracle discards a damaged load and the leg reports %s", s.name, s.unit, s.root, retain, leg)
			}
			continue
		}
		if retain && s.retainedAtLeast > 0 {
			var kept int
			_, after, _ := strings.Cut(want, "retained=")
			if _, err := fmt.Sscanf(after, "%d", &kept); err != nil || kept < s.retainedAtLeast {
				t.Errorf("%s (%s.%s): the oracle retains %s, fewer than the %d this seed exists to exercise", s.name, s.unit, s.root, want, s.retainedAtLeast)
			}
		}
		if retain && s.retainWant != "" {
			want = s.retainWant
		}
		if retain && strings.HasSuffix(s.name, "+unknown") {
			// the planted field is ONE MORE retained record than the seed it
			// was planted in already carries: a seed read by a later build
			// (tblscalars2 over scalars_full) retains its own unknowns too
			base, err := os.ReadFile(filepath.Join(repo, s.wire))
			if err != nil {
				t.Fatal(err)
			}
			baseWant, _ := oracleSeed(t, units[s.unit].unit, s.root, base, true)
			if retainedOf(want) != retainedOf(baseWant)+1 {
				t.Errorf("%s (%s.%s): the planted unknown was not retained by the oracle (%s, unplanted %s), so this seed proves nothing", s.name, s.unit, s.root, want, baseWant)
			}
		}
		if leg != want {
			t.Errorf("%s (%s.%s, retain=%t): the leg reports %s, the oracle %s", s.name, s.unit, s.root, retain, leg, want)
			continue
		}
		if (saveLen < 0) != (encoded == nil) {
			t.Errorf("%s (%s.%s, retain=%t): save refusal differs from oracle", s.name, s.unit, s.root, retain)
		} else if encoded != nil && !bytes.Equal(saved, encoded) {
			t.Errorf("%s (%s.%s, retain=%t): the leg saves %d bytes, the oracle %d, first difference at byte %d",
				s.name, s.unit, s.root, retain, len(saved), len(encoded), firstDiff(saved, encoded))
		}
	}
}

// oracleSeed is the Go engine's answer for one seed: the report in the driver's
// spelling and the bytes the (retaining) pair writes back.
func oracleSeed(t *testing.T, u *ir.Unit, root string, wire []byte, retain bool) (string, []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	def := m.Lookup(root)
	inst := m.New(def)
	var rep tabletext.Report
	var ok bool
	var err error
	store := tablewire.Retain{Capacity: 1 << 20, IdCapacity: (1 << 20) / 8}
	if retain {
		ok, err = tablewire.DecodeRegionRetain(m, inst, wire, &store, &rep)
	} else {
		ok, err = tablewire.DecodeRegion(m, inst, wire, &rep)
	}
	if err != nil && !tablewire.Refused(err) {
		t.Fatalf("the oracle refused the root itself: %v", err)
	}
	decodeRefused := tablewire.Refused(err)
	var encoded []byte
	if retain {
		var saved tabletext.Report
		encoded, err = tablewire.EncodeRetain(m, inst, &store, &saved)
		rep.RetainLost += saved.RetainLost
	} else {
		encoded, err = tablewire.Encode(m, inst)
	}
	if err != nil {
		encoded = nil
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t,%t retained=%d retain_lost=%d",
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed || (!ok && !decodeRefused), decodeRefused,
		rep.Retained, rep.RetainLost), encoded
}

// widenOnRead pins WIDENING ON READ (docs/SPEC-TABLES.md §4): a value written
// under narrow kinds and read under wider ones of the same signedness lands
// exactly, each widened field counts one `widened`, and nothing is clamped or
// mismatched. The values are written by the Go engine under the NARROW
// declaration and read by the Rust leg under the WIDE one.
func widenOnRead(t *testing.T) {
	const narrow = "package widen\n\ntable Probe\n{\n    a int8\n    u uint16\n    f float32\n    s int32\n}\n"
	const wide = "package widen\n\ntable Probe\n{\n    a int32\n    u uint64\n    f float64\n    s int64\n}\n"
	const text = `{"a": -5, "u": 65535, "f": 1.5, "s": -2147483648}`

	wu := unitFrom(t, narrow)
	wm := tabletext.NewModel(wu)
	winst := wm.New(wm.Lookup("Probe"))
	var wrep tabletext.Report
	if !wm.Read(winst, []byte(text), &wrep) {
		t.Fatalf("the narrow value did not read: %+v", wrep)
	}
	wire, err := tablewire.Encode(wm, winst)
	if err != nil {
		t.Fatal(err)
	}

	ru := unitFrom(t, wide)
	rm := tabletext.NewModel(ru)
	rinst := rm.New(rm.Lookup("Probe"))
	var rrep tabletext.Report
	if ok, err := tablewire.DecodeRegion(rm, rinst, wire, &rrep); !ok || err != nil {
		t.Fatalf("the oracle did not read the narrow wire: %v %+v", err, rrep)
	}
	if rrep.Widened != 4 || rrep.Clamped != 0 || rrep.KindMismatch != 0 {
		t.Fatalf("the oracle's widening ledger moved: %+v (want widened=4)", rrep)
	}
	want, err := tablewire.Encode(rm, rinst)
	if err != nil {
		t.Fatal(err)
	}

	modules, err := Generate(ru)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "widen", modules)
	main := `mod widen;
use std::io::{Read, Write};
fn main() {
    let mut wire = Vec::new();
    std::io::stdin().read_to_end(&mut wire).unwrap();
    let mut report = widen::TableReport::default();
    let v = widen::probe_load(&wire, &mut report).expect("load");
    println!("{} {} {} {} {} {} {} {}", report.widened, report.clamped, report.kind_mismatch, report.malformed, v.a, v.u, v.f, v.s);
    std::io::stdout().write_all(&widen::probe_save(&v)).unwrap();
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(wire)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the widening probe failed: %v", err)
	}
	line, saved, _ := bytes.Cut(out, []byte("\n"))
	if got, wantLine := string(line), "4 0 0 false -5 65535 1.5 -2147483648"; got != wantLine {
		t.Errorf("the Rust leg's widening read: %q, want %q (widened clamped kind_mismatch malformed a u f s)", got, wantLine)
	}
	if !bytes.Equal(saved, want) {
		t.Errorf("the widened value saves %d bytes, the oracle %d, first difference at byte %d", len(saved), len(want), firstDiff(saved, want))
	}
}

// retainedOf is the `retained=N` count of one report line, -1 when it has none.
func retainedOf(report string) int {
	_, after, ok := strings.Cut(report, "retained=")
	n := -1
	if ok {
		if _, err := fmt.Sscanf(after, "%d", &n); err != nil {
			return -1
		}
	}
	return n
}
