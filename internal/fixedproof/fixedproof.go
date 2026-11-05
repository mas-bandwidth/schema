// Package fixedproof provides the leg-independent 49-row fixed-table proof
// driver shared across target table backends (L2, S2, X1).
//
// For each of the 49 contract-independent fixed-table roadmap rows (33 value
// rows, fixed-file-roundtrip, and 15 evolution/layout/plan/lineage/file rows),
// the driver compiles and executes a live C++ reference oracle, verifies that
// the target leg's FixedSave bytes match C++'s live reference bytes
// byte-for-byte, verifies that each side loads and re-saves the other's bytes
// byte-equal, and runs both a file negative control and a writer negative
// control under a hard 2 GiB memory ceiling.
package fixedproof

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// HardMemoryLimitBytes is the 2 GiB ceiling enforced around negative controls
// and hostile layout probes so no buggy reader or test can allocate without
// bound.
const HardMemoryLimitBytes = int64(2) << 30

var memLimitMu sync.Mutex

// SetMemoryCeiling sets a 2 GiB memory ceiling for the duration of t and
// restores the previous limits in t.Cleanup. On Linux and macOS the ceiling is
// the kernel's RLIMIT_AS soft limit, which the test process and every child it
// spawns inherit, so the native C++ oracle probes are bounded too.
// debug.SetMemoryLimit adds a matching Go GC target.
func SetMemoryCeiling(t *testing.T) {
	t.Helper()
	memLimitMu.Lock()
	prevTarget := debug.SetMemoryLimit(HardMemoryLimitBytes)
	restoreAddressSpace := setAddressSpaceLimit(uint64(HardMemoryLimitBytes))
	memLimitMu.Unlock()
	t.Cleanup(func() {
		memLimitMu.Lock()
		restoreAddressSpace()
		debug.SetMemoryLimit(prevTarget)
		memLimitMu.Unlock()
	})
}

// RowCase describes one of the 49 contract-independent fixed-table proof rows.
type RowCase struct {
	ID       string
	Schema   string
	Root     string
	Body     int64
	Count    int
	CppSetup string
}

// LegRunner is the interface a target leg (luatable, squirreltable, etc.)
// implements to run the shared 49-row value and negative-control suites.
type LegRunner interface {
	Name() string
	// Save constructs the row's records for tc.ID, verifies FixedMeasure(count),
	// saves them with FixedSave, and returns the wire bytes.
	Save(t *testing.T, tc RowCase, u *ir.Unit) []byte
	// LoadAndResave loads wireBytes with FixedLoad, checks the decoded field
	// values for tc.ID, re-saves them with FixedSave, and returns the re-saved
	// wire bytes. If FixedLoad refuses wireBytes, LoadAndResave returns an error.
	LoadAndResave(t *testing.T, tc RowCase, u *ir.Unit, wireBytes []byte) ([]byte, error)
	// SaveMutated patches the emitted writer or runtime for tc.ID so that one
	// field write or zeroing step is altered while keeping a valid Form 3
	// envelope, and returns the mutated wire bytes.
	SaveMutated(t *testing.T, tc RowCase, u *ir.Unit) ([]byte, error)
}

// LineageEntry is a target-neutral locked fixed layout entry.
type LineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

// LoadCounters is the target-neutral report a load accumulates: unknown
// fields, kind mismatches, clamped values, widened values and duplicate keys.
// A load that reads a file the writer's layout fully covers moves none of
// them; every refusal moves none, as moo's head "u.. k.. c.. w.. d.." holds.
type LoadCounters struct {
	Unknown      int
	KindMismatch int
	Clamped      int
	Widened      int
	Duplicate    int
}

// String renders the counters in the order the fixed-form reports name them.
func (c LoadCounters) String() string {
	return fmt.Sprintf("u%d k%d c%d w%d d%d", c.Unknown, c.KindMismatch, c.Clamped, c.Widened, c.Duplicate)
}

// EvoRunner is the interface a target leg implements to run the shared
// evolution/layout/plan/lineage/file proof suite across the 16 non-value rows.
type EvoRunner interface {
	Name() string
	// LoadLineage generates the unit with the given lineage (nil for plain
	// Generate), loads wireBytes into a batch of capacity (or default capacity
	// when cap < 0), and returns:
	//   - count: number of decoded records (-1 on refusal)
	//   - reason: refusal name ("" or "ok" on success)
	//   - fields: flat map of decoded values (e.g. "r0.a" -> "7")
	//   - resaved: re-saved wire bytes when count >= 0
	//   - counters: the load's unknown/kind-mismatch/clamped/widened/duplicate
	//     counts, stale-reset on entry as the fixed-form report is
	LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry, wireBytes []byte, cap int) (count int, reason string, fields map[string]string, resaved []byte, counters LoadCounters)
	// CheckLineage compiles the whole lineage for root, including the unit's
	// own layout, without loading a file, and returns a target-neutral verdict:
	// "" when every entry compiles, or a non-empty diagnostic when any entry is
	// refused. It names no language-specific reason and checks entries that no
	// file selects.
	CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry) string
	// SaveCustom runs a custom record-construction and FixedSave scenario for
	// rows like optional-values, writing, fixed-closure, compiled-plans (W14),
	// and batch-capacity. The shared scenarios are:
	//   optional_absent_zeros  ?Inner absent; plain { a=11, b=22 }, an
	//                          in-memory opt { a=0x12345678, b=0x23456789 }
	//   live_extents_base      [..3]Small with arr[0]=One and count 1, ?Small
	//                          absent, tail int32 = 77
	//   writing_slack_batch    the two-record slack batch (second record's full
	//                          35 bytes are asserted)
	//   writing_overcount      ArrCount = 4, which the writer must refuse
	//   writing_slack_unzeroed the same batch with the per-record zeroing
	//                          planted out, for the negative control
	//   closure_small_42       Small { x int32 = 7 } with x = 42
	//   outer_nested           Outer with Inner by value, [2]Inner, ?Inner
	//   w14_writer_offsets     the writer-offset table W (Inner { p int32,
	//                          q int16 }; W { a uint8, b int32, inner Inner,
	//                          opt ?int32, arr [..3]int16, s string(5),
	//                          tail uint32 }) with a = 0xA1,
	//                          b = 0x0B0B0B0B, inner.p = 0x1E1E1E1E,
	//                          inner.q = 0x2E2E, opt present = 0x0D0D0D0D,
	//                          arr = { 0x3E3E, 0x4E4E, 0 } with count 2,
	//                          s = "seven", tail = 0x7A7A7A7A
	// A scenario the leg cannot construct returns a non-empty reason.
	SaveCustom(t *testing.T, u *ir.Unit, root string, scenario string) (wireBytes []byte, reason string)
	// HasFixedRoot reports whether Generate(u) emits fixed-form support for root.
	HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool
}

// ParseCheckUnit parses and type-checks a schema string into an *ir.Unit.
func ParseCheckUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse probe.schema: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "probe.schema",
		Name:  "probe.schema",
		Base:  "probe",
		Bytes: []byte(src),
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check probe.schema: %v", cerrs[0])
	}
	return u
}

// UnitRefusal loads src through the compiler and returns its refusal, or "".
func UnitRefusal(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.New().Load([]string{p}); err != nil {
		return err.Error()
	}
	return ""
}

// RootOf is the file's root: the fixed root no other fixed table names by value.
func RootOf(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedFormRoots(u)
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	t.Fatal("no fixed root")
	return ""
}

// LoadRepoUnit loads a schema file relative to the repository root.
func LoadRepoUnit(t *testing.T, repoRoot, rel string) *ir.Unit {
	t.Helper()
	full := filepath.Join(repoRoot, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(full)
	ast, perrs := parser.Parse(full, data)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", full, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  full,
		Name:  base,
		Base:  strings.TrimSuffix(base, ".schema"),
		Bytes: data,
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", full, cerrs[0])
	}
	return u
}

// RootStruct finds the named struct in u.
func RootStruct(t *testing.T, u *ir.Unit, root string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == root {
				return st
			}
		}
	}
	t.Fatalf("no fixed table %s in unit", root)
	return nil
}

// RootFacts returns the layout bytes, wire hash, and record body byte size for st.
func RootFacts(st *ir.Struct) (layout []byte, hash uint64, body int64) {
	walk := ir.TableFixedWalkRoot(st)
	layout = ir.TableFixedLayoutBytes(walk)
	hash = ir.TableFixedLayoutHash(layout, st)
	body = ir.TableFixedTypeBytes(st)
	return layout, hash, body
}

// fieldOffsets returns each root field's byte offset in the record body by
// summing the compiler's own layout walk: each field's subtree is consumed
// whole.
func fieldOffsets(st *ir.Struct) map[string]int {
	walk := ir.TableFixedWalkRoot(st)
	var span func(i int) int
	span = func(i int) int {
		n := 1
		for range walk[i].Children {
			n += span(i + n)
		}
		return n
	}
	out := map[string]int{}
	for i, off := 1, 0; i < len(walk); {
		out[strings.TrimSuffix(walk[i].Note, " ?")] = off
		off += int(walk[i].Size)
		i += span(i)
	}
	return out
}

// LineageEntryOf computes the LineageEntry for table root in u.
func LineageEntryOf(u *ir.Unit, root string) (LineageEntry, bool) {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != root {
			continue
		}
		layout, hash, body := RootFacts(st)
		return LineageEntry{
			Wire:   hash,
			Layout: layout,
			Record: 8 + body,
		}, true
	}
	return LineageEntry{}, false
}

// MakeFixedFile builds a Form 3 fixed file from a header hash, layout bytes,
// and zero or more record bodies (stamping each record with hash).
func MakeFixedFile(hash uint64, layout []byte, bodies ...[]byte) []byte {
	out := make([]byte, 0, 20+len(layout)+len(bodies)*32)
	out = append(out, 3, 0, 0, 0, 0, 0, 0, 0)
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	for _, b := range bodies {
		out = binary.LittleEndian.AppendUint64(out, hash)
		out = append(out, b...)
	}
	return out
}

// Cases returns all 49 contract-independent fixed-table proof rows (33 value
// rows, fixed-file-roundtrip, and 15 evolution/layout/plan/lineage/file rows).
func Cases() []RowCase {
	return []RowCase{
		// 1. bool-values
		{
			ID: "bool-values",
			Schema: `package probe
fixed table T {
    a bool = false
    b bool = true
}
`,
			Root:  "T",
			Body:  2,
			Count: 2,
			CppSetup: `
    value[1].a = true;
    value[1].b = false;
`,
		},
		// 2. signed-integers
		{
			ID: "signed-integers",
			Schema: `package probe
fixed table T {
    a int8 = -128
    b int16 = -32768
    c int32 = -2147483648
    d int64 = -9223372036854775808
}
`,
			Root:  "T",
			Body:  15,
			Count: 3,
			CppSetup: `
    value[1].a = 127;
    value[1].b = 32767;
    value[1].c = 2147483647;
    value[1].d = 9223372036854775807LL;
    value[2].a = -1;
    value[2].b = 0;
    value[2].c = 1;
    value[2].d = -2LL;
`,
		},
		// 3. unsigned-integers
		{
			ID: "unsigned-integers",
			Schema: `package probe
fixed table T {
    a uint8 = 255
    b uint16 = 65535
    c uint32 = 4294967295
    d uint64 = 18446744073709551615
}
`,
			Root:  "T",
			Body:  15,
			Count: 3,
			CppSetup: `
    value[1].a = 0;
    value[1].b = 0;
    value[1].c = 0;
    value[1].d = 0ULL;
    value[2].a = 1;
    value[2].b = 256;
    value[2].c = 65536U;
    value[2].d = 4294967296ULL;
`,
		},
		// 4. signed-128
		{
			ID: "signed-128",
			Schema: `package probe
fixed table T {
    a int128 = -170141183460469231731687303715884105728 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
}
`,
			Root:  "T",
			Body:  16,
			Count: 3,
			CppSetup: `
    value[1].a = serialize::int128_t( ( serialize::uint128_t( 0x7fffffffffffffffull ) << 64 ) | serialize::uint128_t( 0xffffffffffffffffull ) );
    value[2].a = serialize::int128_t( 1 );
`,
		},
		// 5. unsigned-128
		{
			ID: "unsigned-128",
			Schema: `package probe
fixed table T {
    a uint128 = 340282366920938463463374607431768211455
}
`,
			Root:  "T",
			Body:  16,
			Count: 3,
			CppSetup: `
    value[1].a = serialize::uint128_t( 0 );
    value[2].a = serialize::uint128_t( 1 );
`,
		},
		// 6. ranged-integers
		{
			ID: "ranged-integers",
			Schema: `package probe
fixed table T {
    a int32 | min = -5, max = 5
    b int64 | min = -1000000000000, max = 1000000000000
    c uint16 | min = 0, max = 1000
}
`,
			Root:  "T",
			Body:  14,
			Count: 2,
			CppSetup: `
    value[0].a = -5;
    value[0].b = -1000000000000LL;
    value[0].c = 0;
    value[1].a = 5;
    value[1].b = 1000000000000LL;
    value[1].c = 1000;
`,
		},
		// 7. bits-values
		{
			ID: "bits-values",
			Schema: `package probe
fixed table T {
    a bits(3)
    b bits(12)
    c bits(20)
    d bits(40)
}
`,
			Root:  "T",
			Body:  20,
			Count: 2,
			CppSetup: `
    value[0].a = 7;
    value[0].b = 4095;
    value[0].c = 1048575U;
    value[0].d = 1099511627775ULL;
    value[1].a = 0;
    value[1].b = 0;
    value[1].c = 0;
    value[1].d = 0ULL;
`,
		},
		// 8. float32-values
		{
			ID: "float32-values",
			Schema: `package probe
fixed table T {
    a float32 = 3.5
    b float32 = -0.5
}
`,
			Root:  "T",
			Body:  8,
			Count: 2,
			CppSetup: `
    value[1].a = -1024.25f;
    value[1].b = 2048.75f;
`,
		},
		// 9. float64-values
		{
			ID: "float64-values",
			Schema: `package probe
fixed table T {
    a float64 = 2.5
    b float64 = -1234.5
}
`,
			Root:  "T",
			Body:  16,
			Count: 2,
			CppSetup: `
    value[1].a = -987654321.125;
    value[1].b = 123456789.875;
`,
		},
		// 10. compressed-floats
		{
			ID: "compressed-floats",
			Schema: `package probe
fixed table T {
    a float32 = 0.5 | min = 0.0, max = 1.0, resolution = 0.01
    b float32 = 0.5 | min = -1.0, max = 1.0, resolution = 0.001
}
`,
			Root:  "T",
			Body:  8,
			Count: 3,
			CppSetup: `
    value[0].a = 0.25f;
    value[0].b = -0.5f;
    value[1].a = 0.0f;
    value[1].b = -1.0f;
    value[2].a = 1.0f;
    value[2].b = 1.0f;
`,
		},
		// 11. signed-fixed-point
		{
			ID: "signed-fixed-point",
			Schema: `package probe
fixed table T {
    a fixed(16, 16) | min = -8, max = 8
    b fixed(48, 16) | min = -100, max = 100
    c fixed(112, 16) | min = -1000, max = 1000
}
`,
			Root:  "T",
			Body:  28,
			Count: 2,
			CppSetup: `
    value[0].a = -( 8 << 16 );
    value[0].b = -( 100LL << 16 );
    value[0].c = serialize::int128_t( ( serialize::uint128_t( 0xffffffffffffffffull ) << 64 ) | serialize::uint128_t( 0xffffffffff000000ull ) );
    value[1].a = ( 8 << 16 );
    value[1].b = ( 100LL << 16 );
    value[1].c = serialize::int128_t( 0x0000000000fa0000ull );
`,
		},
		// 12. unsigned-fixed-point
		{
			ID: "unsigned-fixed-point",
			Schema: `package probe
fixed table T {
    a ufixed(16, 16) | min = 0, max = 16
    b ufixed(48, 16) | min = 0, max = 100
    c ufixed(112, 16) | min = 0, max = 1000
}
`,
			Root:  "T",
			Body:  28,
			Count: 2,
			CppSetup: `
    value[0].a = 0;
    value[0].b = 0ULL;
    value[0].c = serialize::uint128_t( 0 );
    value[1].a = ( 16U << 16 );
    value[1].b = ( 100ULL << 16 );
    value[1].c = serialize::uint128_t( 1000ULL << 16 );
`,
		},
		// 13. flags-values
		{
			ID: "flags-values",
			Schema: `package probe
flags Caps { Jump, Fly, Swim }
fixed table T { caps Caps }
`,
			Root:  "T",
			Body:  8,
			Count: 3,
			CppSetup: `
    value[0].caps = 0;
    value[1].caps = 5;
    value[2].caps = 7;
`,
		},
		// 14. enum-values
		{
			ID: "enum-values",
			Schema: `package probe
enum Grade { Gold, Silver, Bronze }
fixed table T { g Grade }
`,
			Root:  "T",
			Body:  1,
			Count: 3,
			CppSetup: `
    value[0].g = Grade::None;
    value[1].g = Grade::Gold;
    value[2].g = Grade::Bronze;
`,
		},
		// 15. utf8-values
		{
			ID: "utf8-values",
			Schema: `package probe
fixed table T { s string(8) }
`,
			Root:  "T",
			Body:  12,
			Count: 2,
			CppSetup: `
    memcpy( value[1].s, "hello", 5 );
    value[1].s_length = 5;
`,
		},
		// 16. utf16-values
		{
			ID: "utf16-values",
			Schema: `package probe
fixed table T { w wstring(4) }
`,
			Root:  "T",
			Body:  12,
			Count: 2,
			CppSetup: `
    value[1].w[0] = 0x0041;
    value[1].w[1] = 0x03b1;
    value[1].w[2] = 0xd83d;
    value[1].w[3] = 0xde00;
    value[1].w_length = 4;
`,
		},
		// 17. byte-buffers
		{
			ID: "byte-buffers",
			Schema: `package probe
fixed table T { blob bytes(6) }
`,
			Root:  "T",
			Body:  10,
			Count: 2,
			CppSetup: `
    value[1].blob[0] = 0xff;
    value[1].blob[1] = 0x00;
    value[1].blob[2] = 0x5a;
    value[1].blob[3] = 1;
    value[1].blob[4] = 2;
    value[1].blob[5] = 3;
    value[1].blob_length = 6;
`,
		},
		// 18. nested-types
		{
			ID: "nested-types",
			Schema: `package probe
type Badge { title string(4) }
fixed table T { badge Badge }
`,
			Root:  "T",
			Body:  8,
			Count: 1,
			CppSetup: `
    value[0].badge.title[0] = 'h';
    value[0].badge.title[1] = 'i';
    value[0].badge.title_length = 2;
`,
		},
		// 19. nested-fixed-tables
		{
			ID: "nested-fixed-tables",
			Schema: `package probe
fixed table Child { n int32 }
fixed table T { child Child }
`,
			Root:  "T",
			Body:  4,
			Count: 2,
			CppSetup: `
    value[0].child.n = -2147483647 - 1;
    value[1].child.n = 2147483647;
`,
		},
		// 20. fixed-arrays
		{
			ID: "fixed-arrays",
			Schema: `package probe
fixed table T { tag [3]int32 }
`,
			Root:  "T",
			Body:  12,
			Count: 1,
			CppSetup: `
    value[0].tag[0] = -2147483647 - 1;
    value[0].tag[1] = 0;
    value[0].tag[2] = 2147483647;
`,
		},
		// 21. counted-arrays
		{
			ID: "counted-arrays",
			Schema: `package probe
fixed table T { nums [..4]uint16 }
`,
			Root:  "T",
			Body:  12,
			Count: 2,
			CppSetup: `
    value[1].nums[0] = 10;
    value[1].nums[1] = 65535;
    value[1].nums[2] = 32768;
    value[1].nums_count = 3;
`,
		},
		// 22. keyed-arrays
		{
			ID: "keyed-arrays",
			Schema: `package probe
enum Key { A, B }
fixed table T { slots [Key]int32 }
`,
			Root:  "T",
			Body:  8,
			Count: 1,
			CppSetup: `
    value[0].slots[Key::A] = -2147483647 - 1;
    value[0].slots[Key::B] = 2147483647;
`,
		},
		// 23. nested-keyed-arrays
		{
			ID: "nested-keyed-arrays",
			Schema: `package probe
enum Key { A, B }
fixed table Child { n int32 }
fixed table T { slots [Key]Child }
`,
			Root:  "T",
			Body:  8,
			Count: 1,
			CppSetup: `
    value[0].slots[Key::A].n = 11;
    value[0].slots[Key::B].n = -22;
`,
		},
		// 24. union-values
		{
			ID: "union-values",
			Schema: `package probe
type Vec {
    x int32
    y int32
}
fixed table Tab { n int32 }
union Body {
    v Vec
    t Tab
    n int32
}
fixed table T { body Body }
`,
			Root:  "T",
			Body:  9,
			Count: 3,
			CppSetup: `
    value[0].body.type = BodyType::V;
    value[0].body.v.x = 11;
    value[0].body.v.y = -22;
    value[1].body.type = BodyType::T;
    value[1].body.t.n = 41;
    value[2].body.type = BodyType::N;
    value[2].body.n = 1073741821;
`,
		},
		// 25. union-field-arms
		{
			ID: "union-field-arms",
			Schema: `package probe
union Body {
    sa int32
    ta string(4)
    aa [..3]int32
}
fixed table T { body Body }
`,
			Root:  "T",
			Body:  17,
			Count: 3,
			CppSetup: `
    value[0].body.type = BodyType::Sa;
    value[0].body.sa = -5;
    value[1].body.type = BodyType::Ta;
    memcpy( value[1].body.ta.value, "abc", 3 );
    value[1].body.ta.value_length = 3;
    value[2].body.type = BodyType::Aa;
    value[2].body.aa.value[0] = -1;
    value[2].body.aa.value[1] = 0;
    value[2].body.aa.value[2] = 1;
    value[2].body.aa.value_count = 3;
`,
		},
		// 26. payload-free-arms
		{
			ID: "payload-free-arms",
			Schema: `package probe
union Pick {
    alpha int32
    quiet
    signal
}
fixed table T {
    lead int32
    pick Pick
    trail int32
}
`,
			Root:  "T",
			Body:  13,
			Count: 3,
			CppSetup: `
    value[0].lead = 10;
    value[0].pick.type = PickType::Alpha;
    value[0].pick.alpha = 99;
    value[0].trail = 20;
    value[1].lead = 30;
    value[1].pick.alpha = 777;
    value[1].pick.type = PickType::Quiet;
    value[1].trail = 40;
    value[2].lead = 50;
    value[2].pick.alpha = 888;
    value[2].pick.type = PickType::Signal;
    value[2].trail = 60;
`,
		},
		// 27. union-arrays
		{
			ID: "union-arrays",
			Schema: `package probe
union Pick {
    a int32
    b int64
}
fixed table T { picks [..3]Pick }
`,
			Root:  "T",
			Body:  31,
			Count: 1,
			CppSetup: `
    value[0].picks_count = 3;
    value[0].picks[0].type = PickType::A;
    value[0].picks[0].a = 3;
    value[0].picks[1].type = PickType::B;
    value[0].picks[1].b = -1099511627776LL;
    value[0].picks[2].type = PickType::A;
    value[0].picks[2].a = 0;
`,
		},
		// 28. optional-scalars
		{
			ID: "optional-scalars",
			Schema: `package probe
enum Grade { Gold, Silver }
fixed table T {
    n ?int32
    g ?Grade
}
`,
			Root:  "T",
			Body:  7,
			Count: 2,
			CppSetup: `
    value[1].n = -123456;
    value[1].n_present = true;
    value[1].g = Grade::Silver;
    value[1].g_present = true;
`,
		},
		// 29. optional-nesting
		{
			ID: "optional-nesting",
			Schema: `package probe
type Vec {
    x int32
    y int32
}
fixed table T { p ?Vec }
`,
			Root:  "T",
			Body:  9,
			Count: 2,
			CppSetup: `
    value[1].p.x = 99;
    value[1].p.y = -99;
    value[1].p_present = true;
`,
		},
		// 30. optional-arrays
		{
			ID: "optional-arrays",
			Schema: `package probe
fixed table T { xs ?[..3]int32 }
`,
			Root:  "T",
			Body:  17,
			Count: 2,
			CppSetup: `
    value[1].xs[0] = 7;
    value[1].xs[1] = -8;
    value[1].xs[2] = 9;
    value[1].xs_count = 3;
    value[1].xs_present = true;
`,
		},
		// 31. scalar-defaults
		{
			ID: "scalar-defaults",
			Schema: `package probe
enum Grade { Gold, Silver, Bronze }
fixed table T {
    x int32 = 7
    g Grade = Silver
    f float32 = 1.5
}
`,
			Root:  "T",
			Body:  9,
			Count: 2,
			CppSetup: `
    value[1].x = -9;
    value[1].g = Grade::Bronze;
    value[1].f = -2.25f;
`,
		},
		// 32. text-bytes-flags-defaults
		{
			ID: "text-bytes-flags-defaults",
			Schema: `package probe
flags Caps { Jump, Fly, Swim }
fixed table T {
    s string(8) = "hi"
    b bytes(4) = "ab"
    c Caps = { Jump, Fly }
}
`,
			Root:  "T",
			Body:  28,
			Count: 2,
			CppSetup: `
    memset( value[1].s, 0, sizeof( value[1].s ) );
    memcpy( value[1].s, "xyz", 3 );
    value[1].s_length = 3;
    value[1].b[0] = 0x00;
    value[1].b[1] = 0xff;
    value[1].b[2] = 0x5a;
    value[1].b[3] = 0x01;
    value[1].b_length = 4;
    value[1].c = 4;
`,
		},
		// 33. record-measurement
		{
			ID: "record-measurement",
			Schema: `package probe
fixed table T {
    a uint32 = 1
    b uint16 = 2
}
`,
			Root:  "T",
			Body:  6,
			Count: 2,
			CppSetup: `
    value[0].a = 10;
    value[0].b = 20;
    value[1].a = 30;
    value[1].b = 40;
`,
		},
		// 34. fixed-file-roundtrip
		{
			ID: "fixed-file-roundtrip",
			Schema: `package probe
fixed table T {
    a int32 = 11
    b string(6) = "ok"
}
`,
			Root:  "T",
			Body:  14,
			Count: 2,
			CppSetup: `
    value[1].a = 22;
    memset( value[1].b, 0, sizeof( value[1].b ) );
    memcpy( value[1].b, "test", 4 );
    value[1].b_length = 4;
`,
		},
		// 35. file-envelope
		{
			ID: "file-envelope",
			Schema: `package probe
fixed table T {
    x int32 = 1
    y int32 = 2
}
`,
			Root:  "T",
			Body:  8,
			Count: 2,
			CppSetup: `
    value[1].x = 10;
    value[1].y = 20;
`,
		},
		// 36. batch-capacity
		{
			ID: "batch-capacity",
			Schema: `package probe
fixed table T {
    x int32 = 1
    y int32 = 2
}
`,
			Root:  "T",
			Body:  8,
			Count: 3,
			CppSetup: `
    value[1].x = 3;
    value[1].y = 4;
    value[2].x = 5;
    value[2].y = 6;
`,
		},
		// 37. plan-selection
		{
			ID: "plan-selection",
			Schema: `package probe
fixed table T {
    x int32 = 11
    y int32 = 22
}
`,
			Root:  "T",
			Body:  8,
			Count: 2,
			CppSetup: `
    value[1].x = 33;
    value[1].y = 44;
`,
		},
		// 38. compiled-plans
		{
			ID: "compiled-plans",
			Schema: `package probe
fixed table T {
    a uint32 = 1
    b int64 = 10
    c int32 = 99
}
`,
			Root:  "T",
			Body:  16,
			Count: 2,
			CppSetup: `
    value[1].a = 100U;
    value[1].b = -20LL;
    value[1].c = 55;
`,
		},
		// 39. definition-hash
		{
			ID: "definition-hash",
			Schema: `package probe
fixed table T {
    x int32 = 5
    b bytes(6) = "hi"
}
`,
			Root:  "T",
			Body:  14,
			Count: 2,
			CppSetup: `
    value[1].x = 9;
    memset( value[1].b, 0, sizeof( value[1].b ) );
    memcpy( value[1].b, "abc", 3 );
    value[1].b_length = 3;
`,
		},
		// 40. fixed-closure
		{
			ID: "fixed-closure",
			Schema: `package probe
type Nested {
    a int16 = 3
    b int16 = 4
}
fixed table T {
    n Nested
    z int32 = 7
}
`,
			Root:  "T",
			Body:  8,
			Count: 2,
			CppSetup: `
    value[1].n.a = 30;
    value[1].n.b = 40;
    value[1].z = 70;
`,
		},
		// 41. retirement
		{
			ID: "retirement",
			Schema: `package probe
fixed table T {
    a int32 = 1
    b int32 = 2
    c int32 = 3
}
`,
			Root:  "T",
			Body:  12,
			Count: 2,
			CppSetup: `
    value[1].a = 10;
    value[1].b = 20;
    value[1].c = 30;
`,
		},
		// 42. retired-runtime
		{
			ID: "retired-runtime",
			Schema: `package probe
fixed table T {
    x int32 = 11
    y int32 = 22
    z int32 = 33
}
`,
			Root:  "T",
			Body:  12,
			Count: 2,
			CppSetup: `
    value[1].x = 44;
    value[1].y = 55;
    value[1].z = 66;
`,
		},
		// 43. optional-values
		{
			ID: "optional-values",
			Schema: `package probe
type Inner {
    a int32 = 5
    b int32 = 6
}
fixed table T {
    plain Inner
    opt ?Inner
}
`,
			Root:  "T",
			Body:  17,
			Count: 2,
			CppSetup: `
    value[1].plain.a = 11;
    value[1].plain.b = 22;
    value[1].opt_present = true;
    value[1].opt.a = 33;
    value[1].opt.b = 44;
`,
		},
		// 44. field-evolution
		{
			ID: "field-evolution",
			Schema: `package probe
fixed table T {
    b int32 = 10 | was = "a"
    c int32 = 20
}
`,
			Root:  "T",
			Body:  8,
			Count: 2,
			CppSetup: `
    value[1].b = 100;
    value[1].c = 200;
`,
		},
		// 45. live-extents
		{
			ID: "live-extents",
			Schema: `package probe
enum Small { One, Two }
fixed table T {
    arr [..3]Small
    opt ?Small
    tail int32 = 77
}
`,
			Root:  "T",
			Body:  13,
			Count: 2,
			CppSetup: `
    value[1].arr_count = 1;
    value[1].arr[0] = Small::One;
    value[1].opt_present = false;
    value[1].tail = 88;
`,
		},
		// 46. writing
		{
			ID: "writing",
			Schema: `package probe
union Pick {
    narrow uint8
    wide uint64
}
fixed table T {
    arr [..3]uint16
    s string(4)
    b bytes(4)
    pick Pick
}
`,
			Root:  "T",
			Body:  35,
			Count: 2,
			CppSetup: `
    value[1].arr_count = 1;
    value[1].arr[0] = 0x1122U;
    value[1].s_length = 1;
    value[1].s[0] = 'A';
    value[1].b_length = 1;
    value[1].b[0] = 0x42U;
    value[1].pick.type = PickType::Narrow;
    value[1].pick.narrow = 7U;
`,
		},
		// 47. union-guards
		{
			ID: "union-guards",
			Schema: `package probe
type Inner {
    x int16
    y int32
}
union Outer {
    a int8
    nested Inner
}
fixed table T {
    u Outer
    tail int32 = 9
}
`,
			Root:  "T",
			Body:  11,
			Count: 2,
			CppSetup: `
    value[1].u.type = OuterType::Nested;
    value[1].u.nested.x = 12;
    value[1].u.nested.y = 0x12345678;
    value[1].tail = 77;
`,
		},
		// 48. plan-execution
		{
			ID: "plan-execution",
			Schema: `package probe
type Sub {
    p int16 = 3
    q int32 = 4
}
fixed table T {
    a int32 = 1
    sub Sub
    xs [..2]int16
}
`,
			Root:  "T",
			Body:  18,
			Count: 2,
			CppSetup: `
    value[1].a = 10;
    value[1].sub.p = 30;
    value[1].sub.q = 40;
    value[1].xs_count = 2;
    value[1].xs[0] = 50;
    value[1].xs[1] = 60;
`,
		},
		// 49. interoperability
		{
			ID: "interoperability",
			Schema: `package probe
fixed table T {
    a int32 = 7
    s string(4) = "hi"
}
`,
			Root:  "T",
			Body:  12,
			Count: 2,
			CppSetup: `
    value[1].a = 42;
    memset( value[1].s, 0, sizeof( value[1].s ) );
    memcpy( value[1].s, "test", 4 );
    value[1].s_length = 4;
`,
		},
	}
}

func resolveSerializeInclude(fallbackDir string) string {
	for _, c := range []string{os.Getenv("SERIALIZE")} {
		if c == "" {
			continue
		}
		if abs, err := filepath.Abs(c); err == nil {
			if _, err := os.Stat(filepath.Join(abs, "serialize.h")); err == nil {
				return abs
			}
		}
	}
	const minSerialize = `#pragma once
#include <stdint.h>
namespace serialize {
typedef __int128 int128_t;
typedef unsigned __int128 uint128_t;
}
`
	_ = os.WriteFile(filepath.Join(fallbackDir, "serialize.h"), []byte(minSerialize), 0o600)
	return fallbackDir
}

// CppOracle compiles the generated C++ fixed-form code for u, runs FixedSave to
// produce the live reference wire image for tc, and returns a verifier that
// runs C++ FixedLoad + FixedSave to prove byte-for-byte agreement.
func CppOracle(t *testing.T, tc RowCase, u *ir.Unit) ([]byte, func(candidate []byte) error) {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("%s: cpp.Generate: %v", tc.ID, err)
	}
	tables, err := cpptable.GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("%s: cpptable.GenerateLineage: %v", tc.ID, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var tableHeader string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("%s: write %s: %v", tc.ID, name, err)
		}
		if strings.HasSuffix(name, "Table.h") {
			tableHeader = name
		}
	}
	if tableHeader == "" {
		t.Fatalf("%s: no Table.h generated", tc.ID)
	}
	serDir := resolveSerializeInclude(dir)
	src := fmt.Sprintf(`#include <cstdio>
#include <cstdlib>
#include <cstring>
#include "%[4]s"

using namespace probe;

static void fill_records( %[1]s * value )
{
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( value[i] );
%[3]s
}

static bool clean( const TableReport & r )
{
    return !( r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate || r.malformed || r.refused );
}

int main( int argc, char ** argv )
{
    static uint8_t buffer[1 << 18];
    static uint8_t cand_buf[1 << 18];
    static uint8_t re_buf[1 << 18];
    static TableFixedEntry plan[4096];
    TableReport r;

    static %[1]s value[%[2]d];
    fill_records( value );
    const int64_t need = %[1]sFixedMeasure( %[2]d );
    const int64_t saved = %[1]sFixedSave( value, %[2]d, buffer, need );
    if ( saved != need ) {
        printf( "cpp save: need=%%lld saved=%%lld\n", (long long) need, (long long) saved );
        return 1;
    }
    static %[1]s back[%[2]d];
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( back[i] );
    memset( &r, 0, sizeof( r ) );
    const int64_t n = %[1]sFixedLoad( back, %[2]d, buffer, saved, plan, 4096, NULL, &r );
    if ( n != %[2]d || !clean( r ) ) {
        printf( "cpp self-load failed: n=%%lld reason=%%d\n", (long long) n, (int) r.reason );
        return 1;
    }
    if ( argc >= 3 && strcmp( argv[1], "save" ) == 0 ) {
        FILE * f = fopen( argv[2], "wb" );
        if ( !f ) return 2;
        if ( (int64_t) fwrite( buffer, 1, (size_t) saved, f ) != saved ) { fclose( f ); return 3; }
        fclose( f );
        return 0;
    }
    if ( argc >= 3 && strcmp( argv[1], "verify" ) == 0 ) {
        FILE * f = fopen( argv[2], "rb" );
        if ( !f ) return 2;
        const int64_t clen = (int64_t) fread( cand_buf, 1, sizeof( cand_buf ), f );
        fclose( f );
        static %[1]s from_cand[%[2]d];
        for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( from_cand[i] );
        memset( &r, 0, sizeof( r ) );
        const int64_t cn = %[1]sFixedLoad( from_cand, %[2]d, cand_buf, clen, plan, 4096, NULL, &r );
        if ( cn != %[2]d || !clean( r ) ) {
            printf( "cpp load of the candidate failed: n=%%lld reason=%%d\n", (long long) cn, (int) r.reason );
            return 4;
        }
        const int64_t re_saved = %[1]sFixedSave( from_cand, %[2]d, re_buf, need );
        if ( re_saved != saved || memcmp( re_buf, buffer, (size_t) saved ) != 0 ) {
            printf( "cpp re-save of the candidate is not the reference's bytes\n" );
            return 5;
        }
        if ( clen != saved || memcmp( cand_buf, buffer, (size_t) saved ) != 0 ) {
            printf( "the candidate is not the reference's bytes\n" );
            return 6;
        }
        return 0;
    }
    return 1;
}
`, tc.Root, tc.Count, tc.CppSetup, tableHeader)
	probeCpp := filepath.Join(dir, "probe.cpp")
	probeBin := filepath.Join(dir, "probe")
	if err := os.WriteFile(probeCpp, []byte(src), 0o600); err != nil {
		t.Fatalf("%s: write probe.cpp: %v", tc.ID, err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if out, err := exec.Command(cxx, "-std=c++17", "-O1", "-I"+dir, "-I"+serDir, probeCpp, "-o", probeBin).CombinedOutput(); err != nil {
		t.Fatalf("%s: the C++ reference does not compile: %v\n%s", tc.ID, err, out)
	}
	cppBin := filepath.Join(dir, "cpp.bin")
	if out, err := exec.Command(probeBin, "save", cppBin).CombinedOutput(); err != nil {
		t.Fatalf("%s: the C++ reference's save failed: %v\n%s", tc.ID, err, out)
	}
	cppBytes, err := os.ReadFile(cppBin)
	if err != nil {
		t.Fatalf("%s: %v", tc.ID, err)
	}
	verify := func(cand []byte) error {
		candPath := filepath.Join(dir, "cand.bin")
		if err := os.WriteFile(candPath, cand, 0o600); err != nil {
			return err
		}
		out, err := exec.Command(probeBin, "verify", candPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return cppBytes, verify
}

// RunValueSuite drives all 49 rows of Cases() against runner:
//   - verifies layout hash and record body size against ir.TableFixedWalkRoot
//   - compiles and executes the live C++ reference oracle
//   - verifies leg FixedSave bytes equal C++ FixedSave bytes byte-for-byte
//   - verifies leg FixedLoad(cppBytes) + FixedSave equals cppBytes byte-for-byte
//   - verifies C++ FixedLoad(legBytes) + FixedSave equals legBytes byte-for-byte
func RunValueSuite(t *testing.T, runner LegRunner) {
	SetMemoryCeiling(t)
	for _, tc := range Cases() {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			u := ParseCheckUnit(t, tc.Schema)
			st := RootStruct(t, u, tc.Root)
			layout, hash, body := RootFacts(st)
			if body != tc.Body {
				t.Fatalf("%s: body bytes = %d, want %d", tc.ID, body, tc.Body)
			}
			if len(layout) < 4+17 || hash == 0 {
				t.Fatalf("%s: invalid layout (%d bytes) or zero hash", tc.ID, len(layout))
			}

			cppBytes, verifyInCpp := CppOracle(t, tc, u)
			legBytes := runner.Save(t, tc, u)
			if !bytes.Equal(legBytes, cppBytes) {
				t.Fatalf("%s: %s FixedSave bytes != C++ reference FixedSave bytes:\nleg=%x\ncpp=%x", tc.ID, runner.Name(), legBytes, cppBytes)
			}

			resaved, err := runner.LoadAndResave(t, tc, u, cppBytes)
			if err != nil {
				t.Fatalf("%s: %s FixedLoad of C++ reference bytes failed: %v", tc.ID, runner.Name(), err)
			}
			if !bytes.Equal(resaved, cppBytes) {
				t.Fatalf("%s: %s re-save after loading C++ bytes differed:\ngot =%x\nwant=%x", tc.ID, runner.Name(), resaved, cppBytes)
			}

			if err := verifyInCpp(legBytes); err != nil {
				t.Fatalf("%s: C++ FixedLoad verification of %s bytes failed: %v", tc.ID, runner.Name(), err)
			}
		})
	}
}

// RunNegativeControlSuite drives both the file negative control and the writer
// negative control across all 49 rows of Cases() under a hard 2 GiB memory
// ceiling.
func RunNegativeControlSuite(t *testing.T, runner LegRunner) {
	SetMemoryCeiling(t)
	for _, tc := range Cases() {
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			u := ParseCheckUnit(t, tc.Schema)
			st := RootStruct(t, u, tc.Root)
			layout, _, body := RootFacts(st)
			cppBytes, verifyInCpp := CppOracle(t, tc, u)

			// 1. File negative control (envelope damage): corrupting the form byte
			// must be rejected by both C++ FixedLoad and the leg's reader.
			corruptForm := bytes.Clone(cppBytes)
			corruptForm[0] ^= 0x01
			if err := verifyInCpp(corruptForm); err == nil {
				t.Fatalf("%s: file negative control failed: C++ FixedLoad accepted corrupted form byte", tc.ID)
			}
			if _, err := runner.LoadAndResave(t, tc, u, corruptForm); err == nil {
				t.Fatalf("%s: file negative control failed: %s FixedLoad accepted corrupted form byte", tc.ID, runner.Name())
			}

			// 2. File negative control (record payload mutation): flipping a live
			// byte in the last record body must be rejected by the C++ reference
			// oracle and must not round-trip back to cppBytes in the leg.
			mutatedPayload := bytes.Clone(cppBytes)
			lastRecBodyStart := 20 + len(layout) + (tc.Count-1)*int(8+body) + 8
			if lastRecBodyStart >= len(mutatedPayload) || body <= 0 {
				t.Fatalf("%s: unexpected record body offset %d (len=%d)", tc.ID, lastRecBodyStart, len(mutatedPayload))
			}
			mutatedPayload[lastRecBodyStart] ^= 0x01
			if err := verifyInCpp(mutatedPayload); err == nil {
				t.Fatalf("%s: file negative control failed: C++ oracle accepted mutated payload byte", tc.ID)
			}
			if resaved, err := runner.LoadAndResave(t, tc, u, mutatedPayload); err == nil && bytes.Equal(resaved, cppBytes) {
				t.Fatalf("%s: file negative control failed: %s LoadAndResave of mutated payload matched original cppBytes", tc.ID, runner.Name())
			}

			// 3. Writer negative control: mutating the leg's writer/runtime for
			// tc.ID must either fail or produce bytes rejected by C++ reference
			// equality.
			mutatedWrite, err := runner.SaveMutated(t, tc, u)
			if err == nil {
				if bytes.Equal(mutatedWrite, cppBytes) {
					t.Fatalf("%s: writer negative control failed: mutated %s writer still produced C++ oracle bytes", tc.ID, runner.Name())
				}
				if err := verifyInCpp(mutatedWrite); err == nil {
					t.Fatalf("%s: writer negative control failed: C++ oracle accepted mutated %s writer output", tc.ID, runner.Name())
				}
			}
		})
	}
}

type corpusVector struct {
	key, schema, root, file string
}

var fixedCorpusVectors = []corpusVector{
	{"fx1", "test/tables/FX1.schema", "FxRoot", "fx1.bin"},
	{"fx2", "test/tables/FX2.schema", "FxRoot", "fx2.bin"},
	{"p1", "test/tables/P1.schema", "Chain", "p1.bin"},
	{"p3", "test/tables/P3.schema", "Chain", "p3.bin"},
	{"fxw", "test/tables/FXW.schema", "FxWide", "fxw.bin"},
}

func findFixedCorpus(t *testing.T, repoRoot string) (string, map[string]int) {
	t.Helper()
	corpus := filepath.Join(repoRoot, "build", "fixedform-corpus")
	mPath := filepath.Join(corpus, "manifest.txt")
	data, err := os.ReadFile(mPath)
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is absent: %v", mPath, err)
		}
		t.Skipf("fixedform-corpus not built (%v)", err)
	}
	records := map[string]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "file=") {
			continue
		}
		var file string
		n := -1
		for f := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			if k == "file" {
				file = v
			} else if k == "records" {
				n, _ = strconv.Atoi(v)
			}
		}
		if file != "" && n >= 0 {
			records[file] = n
		}
	}
	return corpus, records
}

// manifestLine is one corpus manifest line: its record count and the values the
// reference landed, normalised so a float compares by its bits and a
// fixed-point value by its raw integer.
type manifestLine struct {
	records int
	values  map[string]string
}

var manifestValueSplit = regexp.MustCompile(`,(r\d+\.)`)

func readFixedManifest(t *testing.T, corpus string) map[string]manifestLine {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]manifestLine{}
	for line := range strings.SplitSeq(string(data), "\n") {
		head, values, _ := strings.Cut(line, " values=")
		var file string
		ml := manifestLine{values: map[string]string{}}
		for field := range strings.FieldsSeq(head) {
			k, v, _ := strings.Cut(field, "=")
			switch k {
			case "file":
				file = v
			case "records":
				ml.records, _ = strconv.Atoi(v)
			}
		}
		if file == "" {
			continue
		}
		// a value may hold a comma inside its quotes: split only before the
		// next r<k>. key
		for kv := range strings.SplitSeq(manifestValueSplit.ReplaceAllString(values, "\x00$1"), "\x00") {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				ml.values[k] = normaliseManifestValue(v)
			}
		}
		out[file] = ml
	}
	return out
}

func normaliseManifestValue(v string) string {
	if _, raw, ok := strings.Cut(v, "|raw="); ok {
		return "raw=" + raw
	}
	if _, bits, ok := strings.Cut(v, "|0x"); ok {
		return "0x" + strings.ToUpper(bits)
	}
	return v
}

// compareManifest holds every manifest value the reader names to the value it
// landed and answers how many were compared. A key the reader does not name (a
// field it does not declare, a wide string's units) is not compared.
func compareManifest(t *testing.T, what string, ml manifestLine, dump map[string]string) int {
	t.Helper()
	compared := 0
	for k, want := range ml.values {
		got, ok := dump[k]
		if !ok || strings.HasPrefix(want, `u"`) || strings.Contains(want, "\\") {
			continue
		}
		if raw, ok := strings.CutPrefix(got, "raw="); ok && !strings.HasPrefix(want, "raw=") {
			got = raw
		}
		if widened, ok := widenedFloat32(want, got); ok {
			want = widened
		}
		if got != want {
			t.Errorf("%s: %s landed %s, the reference wrote %s", what, k, got, want)
		}
		compared++
	}
	return compared
}

// widenedFloat32 is the float64 pattern a float32 pattern widens to, when the
// reference wrote a float32 (0x and eight digits) and the read landed a float64
// (0x and sixteen): the ordinary conversion, exact for every finite float32.
func widenedFloat32(want, got string) (string, bool) {
	if len(want) != 10 || len(got) != 18 || !strings.HasPrefix(want, "0x") || !strings.HasPrefix(got, "0x") {
		return "", false
	}
	bits, err := strconv.ParseUint(want[2:], 16, 32)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("0x%016X", math.Float64bits(float64(math.Float32frombits(uint32(bits))))), true
}

// zeroLoad is the common load: it asserts, as moo's refusedTotal and every
// "u0 k0 c0 w0 d0" head does, that the load moved no report counter, and hands
// back the values the row checks.
func zeroLoad(t *testing.T, runner EvoRunner, u *ir.Unit, root string, lineage map[string][]LineageEntry, wireBytes []byte, cap int) (int, string, map[string]string, []byte) {
	t.Helper()
	n, reason, fields, resaved, counters := runner.LoadLineage(t, u, root, lineage, wireBytes, cap)
	if counters != (LoadCounters{}) {
		t.Errorf("load of %s moved counters %s, want u0 k0 c0 w0 d0", root, counters)
	}
	return n, reason, fields, resaved
}

// RunEvolutionSuite drives the 16 non-value fixed-table rows (fixed-file-roundtrip
// and the 15 evolution/layout/plan/lineage/file rows) against runner.
func RunEvolutionSuite(t *testing.T, repoRoot string, runner EvoRunner) {
	SetMemoryCeiling(t)

	// 34. fixed-file-roundtrip
	t.Run("fixed-file-roundtrip", func(t *testing.T) {
		t.Parallel()
		corpus, records := findFixedCorpus(t, repoRoot)
		for _, v := range fixedCorpusVectors {
			want := records[v.file]
			data, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatal(err)
			}
			u := LoadRepoUnit(t, repoRoot, v.schema)
			st := RootStruct(t, u, v.root)
			layout, hash, body := RootFacts(st)
			if got := binary.LittleEndian.Uint64(data[8:16]); got != hash {
				t.Fatalf("%s: header hash 0x%016x != 0x%016x", v.file, got, hash)
			}
			if len(data)-20-len(layout) != want*int(8+body) {
				t.Fatalf("%s: record byte size mismatch", v.file)
			}
			n, reason, _, resaved := zeroLoad(t, runner, u, v.root, nil, data, -1)
			if n != want || !bytes.Equal(resaved, data) {
				t.Fatalf("%s: roundtrip failed: n=%d (want %d) reason=%q bytesEqual=%v", v.file, n, want, reason, bytes.Equal(resaved, data))
			}
		}
	})

	// 35. file-envelope (F1, F2, F3, F4, F7, F8, F12)
	t.Run("file-envelope", func(t *testing.T) {
		t.Parallel()
		corpus, _ := findFixedCorpus(t, repoRoot)
		oracle, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
		if err != nil {
			t.Fatal(err)
		}
		u := LoadRepoUnit(t, repoRoot, "test/tables/FX1.schema")
		eOwn, ok := LineageEntryOf(u, "FxRoot")
		if !ok {
			t.Fatal("missing FxRoot lineage entry")
		}
		lin := map[string][]LineageEntry{"FxRoot": {eOwn}}
		cut := func(fn func(b []byte)) []byte { b := bytes.Clone(oracle); fn(b); return b }
		for _, c := range []struct {
			name string
			file []byte
		}{
			{"F1_form_0", cut(func(b []byte) { b[0] = 0 })},
			{"F1_form_1", cut(func(b []byte) { b[0] = 1 })},
			{"F2_form_2", cut(func(b []byte) { b[0] = 2 })},
			{"F3_form_4", cut(func(b []byte) { b[0] = 4 })},
			{"F3_form_255", cut(func(b []byte) { b[0] = 255 })},
			{"F4_layout_past_file", cut(func(b []byte) { binary.LittleEndian.PutUint32(b[16:], 0x7fff0000) })},
			{"F7_under_20_bytes", oracle[:19]},
			{"F8_ragged_tail", oracle[:len(oracle)-1]},
			{"F12_layout_byte_moved", cut(func(b []byte) { b[20] ^= 1 })},
			{"F12_layout_one_short", cut(func(b []byte) { b[16]-- })},
		} {
			n, reason, _, _ := zeroLoad(t, runner, u, "FxRoot", lin, c.file, -1)
			if n != -1 {
				t.Errorf("%s: got n=%d (reason=%q), want -1 (refused)", c.name, n, reason)
			}
		}
		n, reason, _, _ := zeroLoad(t, runner, u, "FxRoot", lin, oracle, -1)
		if n != 2 {
			t.Errorf("control: got n=%d (reason=%q), want 2", n, reason)
		}
	})

	// 36. batch-capacity (F9)
	t.Run("batch-capacity", func(t *testing.T) {
		t.Parallel()
		u := ParseCheckUnit(t, "package probe\nfixed table Point {\n    x int32 = 1\n    y int32 = 2\n}\n")
		eOwn, _ := LineageEntryOf(u, "Point")
		lin := map[string][]LineageEntry{"Point": {eOwn}}
		body := make([]byte, 8)
		binary.LittleEndian.PutUint32(body[0:], 10)
		binary.LittleEndian.PutUint32(body[4:], 20)
		three := MakeFixedFile(eOwn.Wire, eOwn.Layout, body, body, body)
		badLast := bytes.Clone(three)
		badLast[len(badLast)-16] ^= 1 // the third record's hash
		bodies4097 := make([][]byte, 4097)
		for i := range bodies4097 {
			bodies4097[i] = body
		}
		four097 := MakeFixedFile(eOwn.Wire, eOwn.Layout, bodies4097...)
		for _, c := range []struct {
			name     string
			file     []byte
			capacity int
			wantN    int
		}{
			{"past the room", three, 2, -1},
			{"past the room, before the record hashes", badLast, 2, -1},
			{"the room exactly", three, 3, 3},
			{"no capacity", three, -1, 3},
			{"the bad hash in the room", badLast, 3, -1},
			{"4097 records with no capacity", four097, -1, 4097},
			{"4097 records past 4096 capacity", four097, 4096, -1},
		} {
			n, reason, _, _ := zeroLoad(t, runner, u, "Point", lin, c.file, c.capacity)
			if n != c.wantN {
				t.Errorf("%s: got n=%d (reason=%q), want %d", c.name, n, reason, c.wantN)
			}
		}
	})

	// 37. plan-selection (R7, R8, R9, F10, R12)
	t.Run("plan-selection", func(t *testing.T) {
		t.Parallel()
		corpus, _ := findFixedCorpus(t, repoRoot)
		oracle, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
		if err != nil {
			t.Fatal(err)
		}
		u := LoadRepoUnit(t, repoRoot, "test/tables/FX1.schema")
		eOwn, _ := LineageEntryOf(u, "FxRoot")
		lin := map[string][]LineageEntry{"FxRoot": {eOwn}}

		unknown := bytes.Clone(oracle)
		unknown[8] ^= 1
		if n, reason, _, _ := zeroLoad(t, runner, u, "FxRoot", lin, unknown, -1); n != -1 {
			t.Errorf("R8 unknown hash: got n=%d (reason=%q), want -1 (refused)", n, reason)
		}

		layoutLen := int(binary.LittleEndian.Uint32(oracle[16:20]))
		for i := 20; i < 20+layoutLen; i++ {
			moved := bytes.Clone(oracle)
			moved[i] ^= 0x5a
			if n, reason, _, _ := zeroLoad(t, runner, u, "FxRoot", lin, moved, -1); n != -1 {
				t.Errorf("R9 layout byte %d moved: got n=%d (reason=%q), want -1 (refused)", i, n, reason)
			}
		}

		rec0 := 20 + layoutLen
		stride := (len(oracle) - rec0) / 2
		last := bytes.Clone(oracle)
		last[rec0+stride] ^= 1
		if n, reason, _, _ := zeroLoad(t, runner, u, "FxRoot", lin, last, -1); n != -1 {
			t.Errorf("F10/R12 last record hash mismatch: got n=%d (reason=%q), want -1 (refused)", n, reason)
		}

		older := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_field_append.schema")
		newer := LoadRepoUnit(t, repoRoot, "test/tables/VNEW_field_append.schema")
		eOld, _ := LineageEntryOf(older, "Lineage")
		oldBin, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		newBin, err := os.ReadFile(filepath.Join(corpus, "new_field_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		linV := map[string][]LineageEntry{"Lineage": {eOld}}
		nOld, rOld, fOld, _ := zeroLoad(t, runner, newer, "Lineage", linV, oldBin, -1)
		if nOld != 1 || fOld["r0.w"] != "77" {
			t.Errorf("older entry plan: n=%d reason=%q w=%q, want 1 77", nOld, rOld, fOld["r0.w"])
		}
		nNew, rNew, _, _ := zeroLoad(t, runner, newer, "Lineage", linV, newBin, -1)
		if nNew != 1 {
			t.Errorf("own entry plan: n=%d reason=%q, want 1", nNew, rNew)
		}
	})

	// 38. compiled-plans (R1, R2, R23, F11, R25, R26, W14)
	t.Run("compiled-plans", func(t *testing.T) {
		t.Parallel()
		u1 := ParseCheckUnit(t, "package probe\nfixed table Lineage {\n    a uint16 = 1\n}\n")
		u2 := ParseCheckUnit(t, "package probe\nfixed table Lineage {\n    a uint32 = 1\n    b int32 = 10\n}\n")
		u3 := ParseCheckUnit(t, "package probe\nfixed table Lineage {\n    a uint32 = 1\n    b int64 = 10\n    c int32 = 99\n}\n")
		e1, _ := LineageEntryOf(u1, "Lineage")
		e2, _ := LineageEntryOf(u2, "Lineage")
		e3, _ := LineageEntryOf(u3, "Lineage")
		if e1.Record != 8+2 || e2.Record != 8+8 || e3.Record != 8+16 {
			t.Fatalf("R2: unexpected lineage record sizes: %d %d %d", e1.Record, e2.Record, e3.Record)
		}
		e0 := e1
		e0.Wire ^= 0xbeef
		e0.Retired = true

		// F11/R25: a layout past the runtime's plan bound (4096 leaves), a
		// lineage entry of 4100 int8 fields under a hash of its own; the
		// compiler caps a declared table's plan at the same bound, so only a
		// lineage carries one.
		const bigFields = 4100
		entryBytes := func(id uint64, kind byte, size, children uint32) []byte {
			b := binary.LittleEndian.AppendUint64(nil, id)
			b = append(b, kind)
			b = binary.LittleEndian.AppendUint32(b, size)
			return binary.LittleEndian.AppendUint32(b, children)
		}
		bigLayout := binary.LittleEndian.AppendUint32(nil, bigFields+1)
		bigLayout = append(bigLayout, entryBytes(0x1111, ir.TableKindTable, bigFields, bigFields)...)
		for i := range bigFields {
			bigLayout = append(bigLayout, entryBytes(0x2000+uint64(i), ir.TableKindI8, 1, 0)...)
		}
		eBig := LineageEntry{Wire: 0xb16b16b16b16b16b, Layout: bigLayout, Record: 8 + bigFields}

		lin := map[string][]LineageEntry{"Lineage": {e0, eBig, e1, e2}}
		v1 := MakeFixedFile(e1.Wire, e1.Layout, []byte{7, 0}, []byte{8, 0})
		b2 := func(a uint32, b int32) []byte {
			x := binary.LittleEndian.AppendUint32(nil, a)
			return binary.LittleEndian.AppendUint32(x, uint32(b))
		}
		v2 := MakeFixedFile(e2.Wire, e2.Layout, b2(100, -20), b2(200, 30))

		n1, r1, f1, _, c1 := runner.LoadLineage(t, u3, "Lineage", lin, v1, -1)
		if n1 != 2 || f1["r0.a"] != "7" || f1["r0.b"] != "10" || f1["r0.c"] != "99" || f1["r1.a"] != "8" {
			t.Errorf("v1: n=%d r=%q fields=%v", n1, r1, f1)
		}
		if want := (LoadCounters{Widened: 2}); c1 != want {
			t.Errorf("v1: counters %s, want %s", c1, want)
		}
		n2, r2, f2, _, c2 := runner.LoadLineage(t, u3, "Lineage", lin, v2, -1)
		if n2 != 2 || f2["r0.a"] != "100" || f2["r0.b"] != "-20" || f2["r0.c"] != "99" || f2["r1.b"] != "30" {
			t.Errorf("v2: n=%d r=%q fields=%v", n2, r2, f2)
		}
		if want := (LoadCounters{Widened: 2}); c2 != want {
			t.Errorf("v2: counters %s, want %s", c2, want)
		}
		if n, r, _, _ := zeroLoad(t, runner, u3, "Lineage", lin, MakeFixedFile(e0.Wire, e0.Layout, []byte{1, 0}), -1); n != -1 {
			t.Errorf("retired: got n=%d r=%q, want -1 (refused)", n, r)
		}
		if n, r, _, _ := zeroLoad(t, runner, u3, "Lineage", lin, MakeFixedFile(0x123456789abcdef0, e3.Layout, make([]byte, 16)), -1); n != -1 {
			t.Errorf("unknown: got n=%d r=%q, want -1 (refused)", n, r)
		}
		// R26: a whole-lineage compile checks every handed entry, including the
		// unit's own layout, so a lineage holding the over-bound entry refuses as
		// a whole while the lawful lineage compiles clean.
		if v := runner.CheckLineage(t, u3, "Lineage", lin); v == "" {
			t.Errorf("R26 over-bound lineage: compiled clean, want a refusal")
		}
		if v := runner.CheckLineage(t, u3, "Lineage", map[string][]LineageEntry{"Lineage": {e1, e2}}); v != "" {
			t.Errorf("R26 lawful lineage including own layout: refused: %s", v)
		}
		if n, r, _, _ := zeroLoad(t, runner, u3, "Lineage", lin, MakeFixedFile(e3.Wire, e3.Layout, make([]byte, 15)), -1); n != -1 {
			t.Errorf("truncated own layout: got n=%d r=%q, want -1 (refused)", n, r)
		}
		if n, r, _, _ := zeroLoad(t, runner, u3, "Lineage", lin, MakeFixedFile(eBig.Wire, eBig.Layout, make([]byte, bigFields)), -1); n != -1 {
			t.Errorf("R25 plan bound: got n=%d r=%q, want -1 (refused)", n, r)
		}

		// W14: every member the save writes at the compiler's offset.
		const w14Schema = "package probe\ntype Inner {\n    p int32\n    q int16\n}\nfixed table W {\n    a uint8\n    b int32\n    inner Inner\n    opt ?int32\n    arr [..3]int16\n    s string(5)\n    tail uint32\n}\n"
		uw := ParseCheckUnit(t, w14Schema)
		file, why := runner.SaveCustom(t, uw, "W", "w14_writer_offsets")
		if why != "" {
			t.Fatalf("W14 save failed: %s", why)
		}
		stW := RootStruct(t, uw, "W")
		off := fieldOffsets(stW)
		for _, name := range []string{"a", "b", "inner", "opt", "arr", "s", "tail"} {
			if _, ok := off[name]; !ok {
				t.Fatalf("W14: the layout walk has no offset for %s: %v", name, off)
			}
		}
		nBody := int(ir.TableFixedTypeBytes(stW))
		if len(file) < nBody {
			t.Fatalf("W14: the save is %d bytes, shorter than the %d-byte body", len(file), nBody)
		}
		body := file[len(file)-nBody:]
		u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(body[at:]) }
		for name, ok := range map[string]bool{
			"a":           body[off["a"]] == 0xA1,
			"b":           u32(off["b"]) == 0x0B0B0B0B,
			"inner":       u32(off["inner"]) == 0x1E1E1E1E,
			"opt_present": body[off["opt"]] == 1,
			"opt":         u32(off["opt"]+1) == 0x0D0D0D0D,
			"arr_count":   u32(off["arr"]) == 2,
			"arr":         u32(off["arr"]+4) == 0x4E4E3E3E,
			"s_length":    u32(off["s"]) == 5,
			"s":           string(body[off["s"]+4:][:5]) == "seven",
			"tail":        u32(off["tail"]) == 0x7A7A7A7A,
		} {
			if !ok {
				t.Errorf("W14: the member %s is not at the layout walk's offset: %v %x", name, off, body)
			}
		}
	})

	// 39. definition-hash (R4, R5, W11, W12)
	t.Run("definition-hash", func(t *testing.T) {
		t.Parallel()
		const schema = "package probe\nfixed table T {\n    b bytes(6)\n    x int32 = 1 | min = -10, max = 10\n    f float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.25\n}\n"
		u := ParseCheckUnit(t, schema)
		st := RootStruct(t, u, "T")
		walk := ir.TableFixedWalkRoot(st)
		if walk[1].Kind != ir.TableKindArray {
			t.Errorf("W11: bytes(6) is layout kind %d, want %d", walk[1].Kind, ir.TableKindArray)
		}
		e, _ := LineageEntryOf(u, "T")
		flipped := bytes.Clone(e.Layout)
		flipped[0] ^= 1
		if ir.TableFixedLayoutHash(flipped, st) == e.Wire {
			t.Errorf("W12: the count word is not inside the hash")
		}
		if d := ir.TableFixedDefinitionsDigest(st); !bytes.Contains(d, []byte{'Q'}) {
			t.Errorf("R5: the digest does not carry the resolution under 'Q': %x", d)
		}
		for _, other := range []string{
			strings.Replace(schema, "max = 10", "max = 11", 1),
			strings.Replace(schema, "resolution = 0.25", "resolution = 0.125", 1),
			strings.Replace(schema, "bytes(6)", "bytes(8)", 1),
			strings.Replace(schema, "    x int32", "    y int32", 1),
		} {
			if _, h, _ := RootFacts(RootStruct(t, ParseCheckUnit(t, other), "T")); h == e.Wire {
				t.Errorf("R5: the hash did not move:\n%s", other)
			}
		}
		// R4: a lineage entry handed under a hash that is not its layout's hash
		// still selects its file: the runtime holds the hash as a constant.
		older := ParseCheckUnit(t, "package probe\nfixed table T {\n    x int32 = 1\n}\n")
		eo, _ := LineageEntryOf(older, "T")
		eo.Wire = 0x0123456789abcdef
		n, r, f, _ := zeroLoad(t, runner, u, "T", map[string][]LineageEntry{"T": {eo}}, MakeFixedFile(eo.Wire, eo.Layout, []byte{5, 0, 0, 0}), -1)
		if n != 1 || f["r0.x"] != "5" {
			t.Errorf("R4: the handed hash: n=%d r=%q fields=%v", n, r, f)
		}
		// the corpus: the compiler's layout and hash are the reference's
		t.Run("corpus", func(t *testing.T) {
			corpus, _ := findFixedCorpus(t, repoRoot)
			for _, v := range fixedCorpusVectors {
				uv := LoadRepoUnit(t, repoRoot, v.schema)
				ev, ok := LineageEntryOf(uv, v.root)
				if !ok {
					t.Fatalf("%s: no fixed lineage entry", v.file)
				}
				data, err := os.ReadFile(filepath.Join(corpus, v.file))
				if err != nil {
					t.Fatal(err)
				}
				if binary.LittleEndian.Uint64(data[8:16]) != ev.Wire || !bytes.Equal(data[20:20+len(ev.Layout)], ev.Layout) {
					t.Errorf("%s: the compiler's layout and hash are not the reference's", v.file)
				}
			}
		})
	})

	// 40. fixed-closure (F5, F6, R5, R6, R14, R16, R22)
	t.Run("fixed-closure", func(t *testing.T) {
		t.Parallel()

		t.Run("R6", func(t *testing.T) {
			t.Parallel()
			u := ParseCheckUnit(t, "package probe\nfixed table Big {\n    blob bytes(70000)\n}\nfixed table Small {\n    x int32 = 7\n}\n")
			warns, errs := ir.TableFixedRecordBounds(u, 0)
			if len(errs) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "Big") || !strings.Contains(warns[0], "65536") {
				t.Fatalf("R6: the ceiling is not named: %v %v", warns, errs)
			}
			if _, ok := LineageEntryOf(u, "Big"); ok {
				t.Errorf("R6: Big has a fixed lineage entry")
			}
			if runner.HasFixedRoot(t, u, "Big") {
				t.Errorf("R6: Big should not be emitted as a fixed root")
			}
			if !runner.HasFixedRoot(t, u, "Small") {
				t.Errorf("R6: Small should be emitted as a fixed root")
			}
			file, why := runner.SaveCustom(t, u, "Small", "closure_small_42")
			if why != "" {
				t.Fatalf("R6: save Small failed: %s", why)
			}
			eSmall, _ := LineageEntryOf(u, "Small")
			n, r, f, _ := zeroLoad(t, runner, u, "Small", map[string][]LineageEntry{"Small": {eSmall}}, file, -1)
			if n != 1 || f["r0.x"] != "42" {
				t.Errorf("R6: Small n=%d r=%q x=%q, want 1 42", n, r, f["r0.x"])
			}
		})

		t.Run("R14_W10", func(t *testing.T) {
			t.Parallel()
			u := ParseCheckUnit(t, "package probe\nfixed table Target {\n    lead uint32 = 1\n    name string(8) = \"ok\"\n    trail uint32 = 2\n}\nfixed table Boundary {\n    lead uint32 = 1\n    name string(8) = \"ok\"\n}\n")
			base, _ := LineageEntryOf(u, "Target")
			bound, _ := LineageEntryOf(u, "Boundary")
			bound.Wire ^= 0x1111
			size := func(e *LineageEntry, i int, n uint32) {
				binary.LittleEndian.PutUint32(e.Layout[4+17*i+9:], n)
			}
			mk := func(x uint64, f func(e *LineageEntry)) LineageEntry {
				e := base
				e.Wire ^= x
				e.Layout = bytes.Clone(base.Layout)
				f(&e)
				e.Record = 8 + int64(binary.LittleEndian.Uint32(e.Layout[4+9:]))
				return e
			}
			hugeField := mk(0x0303, func(e *LineageEntry) { size(e, 0, 65536); size(e, 2, 65537) })
			childSum := mk(0x0404, func(e *LineageEntry) { size(e, 0, 65536); size(e, 2, 65530) })
			reach := mk(0x0505, func(e *LineageEntry) { size(e, 0, 15) })
			lin := map[string][]LineageEntry{"Boundary": {bound}, "Target": {hugeField, childSum, reach}}

			var bb [16]byte
			binary.LittleEndian.PutUint32(bb[0:], 9)
			binary.LittleEndian.PutUint32(bb[4:], 8)
			copy(bb[8:], "edgecase")
			n, r, f, _ := zeroLoad(t, runner, u, "Boundary", lin, MakeFixedFile(bound.Wire, bound.Layout, bb[:]), -1)
			if n != 1 || f["r0.name"] != `"edgecase"` {
				t.Errorf("W10: the entry ending at the record: n=%d r=%q name=%q", n, r, f["r0.name"])
			}
			for name, e := range map[string]LineageEntry{"hugeField": hugeField, "childSum": childSum, "reach": reach} {
				if n, r, _, _ := zeroLoad(t, runner, u, "Target", lin, MakeFixedFile(e.Wire, e.Layout), -1); n != -1 {
					t.Errorf("R14 %s: n=%d r=%q, want -1 (refused)", name, n, r)
				}
			}
		})

		t.Run("R22", func(t *testing.T) {
			t.Parallel()
			u := ParseCheckUnit(t, "package probe\nfixed table Inner {\n    a int32 = 10\n    b string(4) = \"hi\"\n}\nfixed table Outer {\n    head uint32 = 1\n    inner Inner\n    items [2]Inner\n    maybe ?Inner\n    tail uint32 = 2\n}\n")
			file, why := runner.SaveCustom(t, u, "Outer", "outer_nested")
			if why != "" {
				t.Fatalf("R22: save Outer failed: %s", why)
			}
			n, r, f, resaved := zeroLoad(t, runner, u, "Outer", nil, file, -1)
			for k, want := range map[string]string{"r0.head": "7", "r0.inner.a": "11", "r0.inner.b": `"one"`, "r0.items[1].a": "33", "r0.items[1].b": `"six"`, "r0.items[0].b": `"hi"`, "r0.maybe_present": "true", "r0.maybe.a": "44", "r0.tail": "9"} {
				if f[k] != want {
					t.Errorf("R22: %s landed %q, want %s", k, f[k], want)
				}
			}
			if n != 1 || !bytes.Equal(resaved, file) {
				t.Errorf("R22: the nested closure does not round-trip: n=%d r=%q", n, r)
			}
			for _, c := range []struct{ field, want, src string }{
				{"Outer.child", "holds the variable table VarChild by value", "package probe\ntable VarChild { x int32 }\nfixed table Outer { child VarChild }\n"},
				{"Outer.ptr", "is a pointer", "package probe\ntable Node { x int32 }\nfixed table Outer { ptr *Node }\n"},
				{"Outer.m", "is a map", "package probe\nfixed table Outer { m map[uint32]int32 }\n"},
				{"Outer.arr", "is an unbounded array", "package probe\nfixed table Outer { arr []int32 }\n"},
				{"Sub.extra", "sits in an `if` branch", "package probe\ntype Sub {\n    gate bool\n    if gate {\n        extra int32\n    }\n}\nfixed table Outer { sub Sub }\n"},
			} {
				msg := UnitRefusal(t, c.src)
				if !strings.Contains(msg, c.field) || !strings.Contains(msg, c.want) || !strings.Contains(msg, "docs/SPEC-TABLES.md") {
					t.Errorf("R22 %s: the refusal is %q", c.field, msg)
				}
			}
			if UnitRefusal(t, "package probe\nfixed table T { next T }\n") == "" {
				t.Errorf("R22: a fixed table that reaches itself by value is not refused")
			}

			uT := ParseCheckUnit(t, "package probe\nfixed table T {\n    x int32 = 1\n}\n")
			entryBytes := func(id uint64, kind byte, size, children uint32) []byte {
				b := binary.LittleEndian.AppendUint64(nil, id)
				b = append(b, kind)
				b = binary.LittleEndian.AppendUint32(b, size)
				return binary.LittleEndian.AppendUint32(b, children)
			}
			// F6: root size 0 and root size 65537 -> refused
			zeroLay := binary.LittleEndian.AppendUint32(nil, 1)
			zeroLay = append(zeroLay, entryBytes(0x1111, ir.TableKindTable, 0, 0)...)
			eZero := LineageEntry{Wire: 0x1001, Layout: zeroLay, Record: 8}
			overLay := binary.LittleEndian.AppendUint32(nil, 2)
			overLay = append(overLay, entryBytes(0x1111, ir.TableKindTable, 65537, 1)...)
			overLay = append(overLay, entryBytes(0x2222, ir.TableKindI32, 65537, 0)...)
			eOver := LineageEntry{Wire: 0x1002, Layout: overLay, Record: 8 + 65537}
			// R22: unknown kind byte 99 -> refused
			badKindLay := binary.LittleEndian.AppendUint32(nil, 2)
			badKindLay = append(badKindLay, entryBytes(0x1111, ir.TableKindTable, 4, 1)...)
			badKindLay = append(badKindLay, entryBytes(0x2222, 99, 4, 0)...)
			eBadKind := LineageEntry{Wire: 0x1003, Layout: badKindLay, Record: 8 + 4}
			lin := map[string][]LineageEntry{"T": {eZero, eOver, eBadKind}}
			if n, r, _, _ := zeroLoad(t, runner, uT, "T", lin, MakeFixedFile(eZero.Wire, zeroLay), -1); n != -1 {
				t.Errorf("F6 zero size: n=%d r=%q, want -1 (refused)", n, r)
			}
			if n, r, _, _ := zeroLoad(t, runner, uT, "T", lin, MakeFixedFile(eOver.Wire, overLay, make([]byte, 65537)), -1); n != -1 {
				t.Errorf("F6 over size: n=%d r=%q, want -1 (refused)", n, r)
			}
			if n, r, _, _ := zeroLoad(t, runner, uT, "T", lin, MakeFixedFile(eBadKind.Wire, badKindLay, make([]byte, 4)), -1); n != -1 {
				t.Errorf("R22 bad kind: n=%d r=%q, want -1 (refused)", n, r)
			}
		})
	})

	// 41. retirement (R3, R4)
	t.Run("retirement", func(t *testing.T) {
		t.Parallel()
		corpus, _ := findFixedCorpus(t, repoRoot)
		older := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_floor.schema")
		mid := LoadRepoUnit(t, repoRoot, "test/tables/VMID_floor.schema")
		newer := LoadRepoUnit(t, repoRoot, "test/tables/VNEW_floor.schema")
		eOld, _ := LineageEntryOf(older, "Floored")
		eMid, _ := LineageEntryOf(mid, "Floored")

		oldData, _ := os.ReadFile(filepath.Join(corpus, "old_floor.bin"))
		midData, _ := os.ReadFile(filepath.Join(corpus, "mid_floor.bin"))
		newData, _ := os.ReadFile(filepath.Join(corpus, "new_floor.bin"))

		for retire := range 3 {
			l0, l1 := eOld, eMid
			if retire >= 1 {
				l0.Retired = true
			}
			if retire >= 2 {
				l1.Retired = true
			}
			lin := map[string][]LineageEntry{"Floored": {l0, l1}}
			for idx, data := range [][]byte{oldData, midData, newData} {
				n, r, _, _ := zeroLoad(t, runner, newer, "Floored", lin, data, -1)
				if idx < retire {
					if n != -1 {
						t.Errorf("retire=%d idx=%d: got n=%d r=%q, want -1 (refused)", retire, idx, n, r)
					}
				} else {
					if n != 1 {
						t.Errorf("retire=%d idx=%d: got n=%d r=%q, want 1", retire, idx, n, r)
					}
				}
			}
		}
	})

	// 42. retired-runtime (R10, R15)
	t.Run("retired-runtime", func(t *testing.T) {
		t.Parallel()
		corpus, _ := findFixedCorpus(t, repoRoot)
		for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
			older := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_"+row+".schema")
			rootName := older.Files[0].Tables[0].Name
			eOwn, _ := LineageEntryOf(older, rootName)
			lin := map[string][]LineageEntry{rootName: {eOwn}}
			data, err := os.ReadFile(filepath.Join(corpus, "new_"+row+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if n, r, _, _ := zeroLoad(t, runner, older, rootName, lin, data, -1); n != -1 {
				t.Errorf("R15 %s: got n=%d r=%q, want -1 (refused)", row, n, r)
			}
			stranger := bytes.Clone(data)
			binary.LittleEndian.PutUint64(stranger[8:], eOwn.Wire)
			if n, r, _, _ := zeroLoad(t, runner, older, rootName, lin, stranger, -1); n != -1 {
				t.Errorf("R10 %s: got n=%d r=%q, want -1 (refused)", row, n, r)
			}
		}
	})

	// 43. optional-values (E5, W2)
	t.Run("optional-values", func(t *testing.T) {
		t.Parallel()
		const types = "package probe\ntype Inner {\n    a int32 = 5\n    b int32 = 6\n}\n"
		u := ParseCheckUnit(t, types+"fixed table T {\n    plain Inner\n    opt ?Inner\n}\n")
		eOpt, _ := LineageEntryOf(u, "T")
		if eOpt.Record != 8+17 {
			t.Fatalf("E5: record size = %d, want 25", eOpt.Record)
		}
		file, why := runner.SaveCustom(t, u, "T", "optional_absent_zeros")
		if why != "" {
			t.Fatalf("W2 save failed: %s", why)
		}
		body := file[len(file)-17:]
		if body[8] != 0 || !bytes.Equal(body[9:], make([]byte, 8)) {
			t.Errorf("W2: absent optional payload is not zeroed: %x", body)
		}
		nAbsent, rAbsent, fAbsent, _ := zeroLoad(t, runner, u, "T", map[string][]LineageEntry{"T": {eOpt}}, file, -1)
		if nAbsent != 1 || fAbsent["r0.opt_present"] != "false" || fAbsent["r0.plain.b"] != "22" {
			t.Errorf("W2: the absent optional reads back %d %q %v", nAbsent, rAbsent, fAbsent)
		}

		uPlain := ParseCheckUnit(t, types+"fixed table T {\n    plain Inner\n    opt Inner\n}\n")
		ePlain, _ := LineageEntryOf(uPlain, "T")
		pb := make([]byte, 16)
		binary.LittleEndian.PutUint32(pb[8:], 3)
		binary.LittleEndian.PutUint32(pb[12:], 4)
		n, r, f, _ := zeroLoad(t, runner, u, "T", map[string][]LineageEntry{"T": {ePlain}}, MakeFixedFile(ePlain.Wire, ePlain.Layout, pb), -1)
		if n != 1 || f["r0.opt_present"] != "true" || f["r0.opt.b"] != "4" {
			t.Errorf("E5 plain into ?T: n=%d r=%q fields=%v", n, r, f)
		}
	})

	// 44. field-evolution (E6, E8, R19)
	t.Run("field-evolution", func(t *testing.T) {
		t.Parallel()
		corpus, _ := findFixedCorpus(t, repoRoot)
		manifest := readFixedManifest(t, corpus)
		older := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_rename_without_was.schema")
		newer := LoadRepoUnit(t, repoRoot, "test/tables/VNEW_rename_without_was.schema")
		eOld, _ := LineageEntryOf(older, "Lineage")
		eNew, _ := LineageEntryOf(newer, "Lineage")
		if eOld.Wire != eNew.Wire {
			t.Fatalf("E6: rename under was moved hash")
		}
		oldData, err := os.ReadFile(filepath.Join(corpus, "old_rename_without_was.bin"))
		if err != nil {
			t.Fatal(err)
		}
		n, r, f, _ := zeroLoad(t, runner, newer, "Lineage", map[string][]LineageEntry{"Lineage": {eNew}}, oldData, -1)
		want := manifest["old_rename_without_was.bin"].values["r0.a"]
		if n != 1 || want == "" || f["r0.b"] != want {
			t.Errorf("E6: n=%d r=%q b=%q, the writer's a=%q", n, r, f["r0.b"], want)
		}
		for _, row := range []string{"field_deprecate", "field_undeprecate"} {
			o := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_"+row+".schema")
			nu := LoadRepoUnit(t, repoRoot, "test/tables/VNEW_"+row+".schema")
			eo, _ := LineageEntryOf(o, "Lineage")
			en, _ := LineageEntryOf(nu, "Lineage")
			if eo.Wire != en.Wire {
				t.Errorf("E8 %s: hash moved", row)
			}
			for _, c := range []struct {
				u    *ir.Unit
				mod  string
				file string
			}{{nu, "VNEW_" + row, "old_" + row + ".bin"}, {o, "VOLD_" + row, "new_" + row + ".bin"}} {
				data, err := os.ReadFile(filepath.Join(corpus, c.file))
				if err != nil {
					t.Fatal(err)
				}
				if n, r, _, _ := zeroLoad(t, runner, c.u, "Lineage", nil, data, -1); n != 1 {
					t.Errorf("E8 %s %s: n=%d r=%q, want 1", row, c.file, n, r)
				}
			}
		}
		for _, row := range []string{"field_append", "enum_append", "union_append", "flags_append", "nested_append", "keyed_array_enum_append"} {
			o := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_"+row+".schema")
			nu := LoadRepoUnit(t, repoRoot, "test/tables/VNEW_"+row+".schema")
			eo, _ := LineageEntryOf(o, RootOf(t, o))
			file := "old_" + row + ".bin"
			data, err := os.ReadFile(filepath.Join(corpus, file))
			if err != nil {
				t.Fatal(err)
			}
			gotN, gotR, gotF, _ := zeroLoad(t, runner, nu, RootOf(t, o), map[string][]LineageEntry{RootOf(t, o): {eo}}, data, -1)
			if gotN != manifest[file].records {
				t.Errorf("R19 %s: n=%d r=%q, want %d", row, gotN, gotR, manifest[file].records)
			}
			if compareManifest(t, "R19 "+row, manifest[file], gotF) == 0 {
				t.Errorf("R19 %s: no manifest value compared", row)
			}
		}
	})

	// 45. live-extents (C14, W4, W9)
	t.Run("live-extents", func(t *testing.T) {
		t.Parallel()
		u := ParseCheckUnit(t, "package probe\nenum Small { One, Two }\nfixed table T {\n    arr [..3]Small\n    opt ?Small\n    tail int32 = 77\n}\n")
		file, why := runner.SaveCustom(t, u, "T", "live_extents_base")
		if why != "" {
			t.Fatalf("save live_extents_base failed: %s", why)
		}
		body := len(file) - 13
		file[body+5] = 99
		file[body+6] = 99
		file[body+8] = 99
		eOwn, _ := LineageEntryOf(u, "T")
		n, r, f, _ := zeroLoad(t, runner, u, "T", map[string][]LineageEntry{"T": {eOwn}}, file, -1)
		if n != 1 || f["r0.arr_count"] != "1" || f["r0.arr[0]"] != "1" || f["r0.tail"] != "77" || f["r0.opt_present"] != "false" {
			t.Errorf("C14/W4: n=%d r=%q fields=%v", n, r, f)
		}
		corpus, _ := findFixedCorpus(t, repoRoot)
		older := LoadRepoUnit(t, repoRoot, "test/tables/VOLD_field_append.schema")
		newer := LoadRepoUnit(t, repoRoot, "test/tables/VNEW_field_append.schema")
		rootName := RootOf(t, older)
		eOld, _ := LineageEntryOf(older, rootName)
		data, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, f, _ := zeroLoad(t, runner, newer, rootName, map[string][]LineageEntry{rootName: {eOld}}, data, -1); f["r0.w"] != "77" {
			t.Errorf("W9: the unwritten field: %v", f)
		}
	})

	// 46. writing (W1, W3, W5, W8)
	t.Run("writing", func(t *testing.T) {
		t.Parallel()
		u := ParseCheckUnit(t, "package probe\nunion Pick {\n    narrow uint8\n    wide uint64\n}\nfixed table T {\n    arr [..3]uint16\n    s string(4)\n    b bytes(4)\n    pick Pick\n}\n")
		file, why := runner.SaveCustom(t, u, "T", "writing_slack_batch")
		if why != "" {
			t.Fatalf("writing_slack_batch failed: %s", why)
		}
		if len(file) < 35 {
			t.Fatalf("writing_slack_batch: %d bytes, want at least the 35-byte record", len(file))
		}
		body := file[len(file)-35:]
		want := []byte{1, 0, 0, 0, 0x22, 0x11, 0, 0, 0, 0, 1, 0, 0, 0, 'A', 0, 0, 0, 1, 0, 0, 0, 0x42, 0, 0, 0, 1, 7, 0, 0, 0, 0, 0, 0, 0}
		if !bytes.Equal(body, want) {
			t.Errorf("W1/W3/W8: the second record is\n%x, want\n%x", body, want)
		}
		if wire, why := runner.SaveCustom(t, u, "T", "writing_overcount"); why == "" || len(wire) != 0 {
			t.Errorf("W5: an out-of-contract count was written: %d bytes, reason=%q", len(wire), why)
		}
		unzeroed, why := runner.SaveCustom(t, u, "T", "writing_slack_unzeroed")
		if why != "" {
			t.Fatalf("writing_slack_unzeroed failed: %s", why)
		}
		if bytes.Equal(unzeroed, file) {
			t.Errorf("the negative control: an unzeroed record image still writes zero slack")
		}
	})

	// 47. union-guards (W7, W16, R28)
	t.Run("union-guards", func(t *testing.T) {
		t.Parallel()
		const guardOld = "package probe\nunion Inner\n{\n    x int16\n    y int32\n}\nunion Outer\n{\n    inner Inner\n    plain int32\n}\nfixed table T\n{\n    outer Outer\n    tail int32 = 9\n}\n"
		uOld := ParseCheckUnit(t, guardOld)
		uNew := ParseCheckUnit(t, strings.Replace(guardOld, "x int16", "x int32", 1))
		eOld, _ := LineageEntryOf(uOld, "T")
		file := MakeFixedFile(eOld.Wire, eOld.Layout,
			[]byte{1, 1, 0xf9, 0xff, 0, 0, 100, 0, 0, 0},
			[]byte{2, 8, 3, 2, 1, 0, 200, 0, 0, 0})
		n, r, f, _, counters := runner.LoadLineage(t, uNew, "T", map[string][]LineageEntry{"T": {eOld}}, file, -1)
		if n != 2 {
			t.Errorf("W7/W16/R28: n=%d r=%q, want 2", n, r)
		}
		if want := (LoadCounters{Widened: 1}); counters != want {
			t.Errorf("W7/W16/R28: counters %s, want %s", counters, want)
		}
		for k, want := range map[string]string{
			"r0.outer.type": "1", "r0.outer.inner.type": "1", "r0.outer.inner.x": "-7", "r0.tail": "100",
			"r1.outer.type": "2", "r1.outer.plain": "16909064", "r1.tail": "200",
		} {
			if f[k] != want {
				t.Errorf("%s landed %q, want %s", k, f[k], want)
			}
		}
		if _, ok := f["r1.outer.inner.type"]; ok {
			t.Errorf("the unselected inner union landed")
		}
	})

	// 48. plan-execution (W6, P1)
	t.Run("plan-execution", func(t *testing.T) {
		t.Parallel()
		const planSchema = "package probe\nenum Kind { A, B }\nunion Choice\n{\n    n int32\n    s string(4)\n}\nfixed table T\n{\n    head int32 = 1\n    opt ?int32\n    arr [..2]Kind\n    ch Choice\n}\n"
		u := ParseCheckUnit(t, planSchema)
		eOwn, _ := LineageEntryOf(u, "T")
		eForged := eOwn
		eForged.Wire ^= 1
		lin := map[string][]LineageEntry{"T": {eForged}}
		// head 42, opt -99 present, arr {2, 0} with count 1, ch string "hi"
		body := make([]byte, eOwn.Record-8)
		binary.LittleEndian.PutUint32(body[0:], 42)
		body[4] = 1
		binary.LittleEndian.PutUint32(body[5:], 0xffffff9d)
		binary.LittleEndian.PutUint32(body[9:], 1)
		body[13] = 2
		body[15] = 2
		binary.LittleEndian.PutUint32(body[16:], 2)
		copy(body[20:], "hi")
		idBytes := MakeFixedFile(eOwn.Wire, eOwn.Layout, body, body)
		planBytes := bytes.Clone(idBytes)
		planBytes[8] ^= 1
		stride := int(eOwn.Record)
		for at := len(planBytes) - 2*stride; at < len(planBytes); at += stride {
			planBytes[at] ^= 1
		}
		idN, idR, idF, idSaved := zeroLoad(t, runner, u, "T", lin, idBytes, -1)
		planN, planR, planF, planSaved := zeroLoad(t, runner, u, "T", lin, planBytes, -1)
		if idN != 2 || planN != idN || idR != planR || !maps.Equal(idF, planF) ||
			!bytes.Equal(idSaved, idBytes) || !bytes.Equal(planSaved, idBytes) {
			t.Errorf("P1: identity %d %q %v saved=%v, compiled %d %q %v saved=%v", idN, idR, idF, bytes.Equal(idSaved, idBytes), planN, planR, planF, bytes.Equal(planSaved, idBytes))
		}
		if idF["r0.ch.s"] != `"hi"` || idF["r1.opt"] != "-99" {
			t.Errorf("P1: the values: %v", idF)
		}
	})

	// 49. interoperability (W13, P2, R27)
	t.Run("interoperability", func(t *testing.T) {
		t.Parallel()
		corpus, records := findFixedCorpus(t, repoRoot)
		manifest := readFixedManifest(t, corpus)
		for _, v := range fixedCorpusVectors {
			want := records[v.file]
			data, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatal(err)
			}
			u := LoadRepoUnit(t, repoRoot, v.schema)
			st := RootStruct(t, u, v.root)
			layout, hash, _ := RootFacts(st)
			if got := binary.LittleEndian.Uint64(data[8:16]); got != hash {
				t.Fatalf("%s: W13 hash mismatch", v.file)
			}
			lb := int(binary.LittleEndian.Uint32(data[16:20]))
			if lb != len(layout) || !bytes.Equal(data[20:20+lb], layout) {
				t.Fatalf("%s: W13 layout bytes differ", v.file)
			}
			eOwn, _ := LineageEntryOf(u, v.root)
			lin := map[string][]LineageEntry{v.root: {eOwn}}
			n, r, f, resaved := zeroLoad(t, runner, u, v.root, lin, data, -1)
			if n != want || !bytes.Equal(resaved, data) {
				t.Fatalf("%s: P2 write-read-write mismatch: n=%d r=%q", v.file, n, r)
			}
			if compareManifest(t, v.file, manifest[v.file], f) == 0 {
				t.Fatalf("%s: no manifest value compared", v.file)
			}
			moved := bytes.Clone(data)
			moved[20+lb+8] ^= 1
			if mn, _, _, msaved, _ := runner.LoadLineage(t, u, v.root, lin, moved, -1); mn >= 0 && bytes.Equal(msaved, data) {
				t.Fatalf("%s: the negative control: a moved body byte round-trips to the reference's file", v.file)
			}
		}
	})
}
