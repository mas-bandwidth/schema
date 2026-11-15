package rusttable

// A WIRE LENGTH OR REFERENCE IS COMPARED IN u64 BEFORE IT IS NARROWED
// (docs/SPEC-TABLES.md §3, §5; docs/SECURITY.md: the verdict never depends on
// the platform). On a target whose usize is 32 bits a reference of 2^32 + k
// would otherwise name entry k, and a length of 2^32 + k would otherwise mean
// k bytes: a file the 64-bit build refuses could be read. Every reference and
// length here is compared against the ids or bytes left in u64 first, and only
// then cast.
//
// A FUNCTIONAL TEST: it compiles and runs the generated Rust twice, once for
// the host (usize = 64) and once for a 32-bit target rustc has installed, and
// compares both verdicts with the engine's. It skips by name when rustc or a
// 32-bit target is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by
// name when SCHEMA_REQUIRE_CORPUS=1.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const wireLengths32BitSchema = `package probe

enum Color
{
    red
    green
}

union Pick
{
    n int32
    s string(16)
}

table Plain
{
    x int32
}

table EnumRoot
{
    e Color
}

table KeyedRoot
{
    k [Color]int32
}

table UnionRoot
{
    u Pick
}

table ListRoot
{
    items []int32
}
`

type wire32Builder struct {
	ids  []uint64
	body []byte
}

func (w *wire32Builder) intern(id uint64) uint64 {
	for i, v := range w.ids {
		if v == id {
			return uint64(i + 1)
		}
	}
	w.ids = append(w.ids, id)
	return uint64(len(w.ids))
}

func (w *wire32Builder) ref(id uint64) []byte {
	return binary.AppendUvarint(nil, w.intern(id))
}

func (w *wire32Builder) field(id uint64, kind byte, payload []byte) {
	w.body = append(w.body, w.ref(id)...)
	w.body = append(w.body, kind)
	w.body = append(w.body, payload...)
}

func (w *wire32Builder) file() []byte {
	out := []byte{1}
	out = append(out, w.body...)
	out = append(out, 0)
	for _, id := range w.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(w.ids)))
}

func leb32(v uint64) []byte { return binary.AppendUvarint(nil, v) }

type wire32Case struct {
	name string
	root string
	wire []byte
	want bool // the engine's verdict: true when the file reads whole
}

func wire32Cases(u *ir.Unit) []wire32Case {
	fieldID := func(table string) uint64 {
		return ir.TableFieldWireId(u.Tables[table].Fields[0])
	}
	color := u.Tables["EnumRoot"].Fields[0].Type.Ref.(*ir.Enum)
	redID := ir.TableWireId(color.VariantWireName(0))
	pick := u.Tables["UnionRoot"].Fields[0].Type.Ref.(*ir.Union)
	armID := ir.TableWireId(pick.Variants[0].WireName())
	plainX := fieldID("Plain")
	unknown := ir.TableWireId("ProbeUnknownField")

	var cases []wire32Case

	// A known enum field: a control, a field reference past the id table, and
	// a variant reference past it. The reference folds onto entry 1 or 2 on a
	// 32-bit usize, so only the u64 comparison refuses it.
	enumControl := &wire32Builder{}
	enumControl.intern(fieldID("EnumRoot"))
	enumControl.intern(redID)
	enumControl.body = append(enumControl.body, 1, byte(ir.TableKindEnum))
	enumControl.body = append(enumControl.body, leb32(2)...)
	cases = append(cases, wire32Case{"enum/control", "EnumRoot", enumControl.file(), true})

	enumVariant := &wire32Builder{}
	enumVariant.intern(fieldID("EnumRoot"))
	enumVariant.intern(redID)
	enumVariant.body = append(enumVariant.body, 1, byte(ir.TableKindEnum))
	enumVariant.body = append(enumVariant.body, leb32((uint64(1)<<32)+2)...)
	cases = append(cases, wire32Case{"enum/variant-reference", "EnumRoot", enumVariant.file(), false})

	enumField := &wire32Builder{}
	enumField.intern(fieldID("EnumRoot"))
	enumField.intern(redID)
	enumField.body = append(enumField.body, leb32((uint64(1)<<32)+1)...)
	enumField.body = append(enumField.body, byte(ir.TableKindEnum))
	enumField.body = append(enumField.body, leb32(2)...)
	cases = append(cases, wire32Case{"enum/field-reference", "EnumRoot", enumField.file(), false})

	// An unknown field of each reference-shaped kind: an enum reference, a
	// keyed array's key reference and a union's arm reference. Each payload
	// names entry 1 after narrowing, and none does in u64.
	unknownEnum := &wire32Builder{}
	unknownEnum.field(plainX, byte(ir.TableKindI32), []byte{0, 0, 0, 0})
	unknownEnum.field(unknown, byte(ir.TableKindEnum), leb32((uint64(1)<<32)+1))
	cases = append(cases, wire32Case{"unknown/enum-reference", "Plain", unknownEnum.file(), false})

	keyedContent := []byte{byte(ir.TableKindI32), 1}
	keyedContent = append(keyedContent, leb32((uint64(1)<<32)+1)...)
	keyedContent = append(keyedContent, 4, 0, 0, 0, 0)
	unknownKeyed := &wire32Builder{}
	unknownKeyed.field(unknown, byte(ir.TableKindKeyed), append(leb32(uint64(len(keyedContent))), keyedContent...))
	cases = append(cases, wire32Case{"unknown/keyed-reference", "Plain", unknownKeyed.file(), false})

	unionContent := leb32((uint64(1) << 32) + 1)
	unionContent = append(unionContent, byte(ir.TableKindI32), 4, 0, 0, 0, 0)
	unknownUnion := &wire32Builder{}
	unknownUnion.field(unknown, byte(ir.TableKindUnion), unionContent)
	cases = append(cases, wire32Case{"unknown/union-reference", "Plain", unknownUnion.file(), false})

	// A known keyed array and a known union: the same reference past the id
	// table in the generated codec.
	keyedControlContent := []byte{byte(ir.TableKindI32), 1}
	keyedControlContent = append(keyedControlContent, leb32(2)...)
	keyedControlContent = append(keyedControlContent, 4, 0, 0, 0, 0)
	keyedControl := &wire32Builder{}
	keyedControl.intern(fieldID("KeyedRoot"))
	keyedControl.intern(redID)
	keyedControl.body = append(keyedControl.body, 1, byte(ir.TableKindKeyed))
	keyedControl.body = append(keyedControl.body, leb32(uint64(len(keyedControlContent)))...)
	keyedControl.body = append(keyedControl.body, keyedControlContent...)
	cases = append(cases, wire32Case{"keyed/control", "KeyedRoot", keyedControl.file(), true})

	keyedHostileContent := []byte{byte(ir.TableKindI32), 1}
	keyedHostileContent = append(keyedHostileContent, leb32((uint64(1)<<32)+2)...)
	keyedHostileContent = append(keyedHostileContent, 4, 0, 0, 0, 0)
	keyedHostile := &wire32Builder{}
	keyedHostile.intern(fieldID("KeyedRoot"))
	keyedHostile.intern(redID)
	keyedHostile.body = append(keyedHostile.body, 1, byte(ir.TableKindKeyed))
	keyedHostile.body = append(keyedHostile.body, leb32(uint64(len(keyedHostileContent)))...)
	keyedHostile.body = append(keyedHostile.body, keyedHostileContent...)
	cases = append(cases, wire32Case{"keyed/reference", "KeyedRoot", keyedHostile.file(), false})

	unionControlContent := leb32(2)
	unionControlContent = append(unionControlContent, byte(ir.TableKindI32), 4, 0, 0, 0, 0)
	unionControl := &wire32Builder{}
	unionControl.intern(fieldID("UnionRoot"))
	unionControl.intern(armID)
	unionControl.body = append(unionControl.body, 1, byte(ir.TableKindUnion))
	unionControl.body = append(unionControl.body, unionControlContent...)
	cases = append(cases, wire32Case{"union/control", "UnionRoot", unionControl.file(), true})

	unionHostileContent := leb32((uint64(1) << 32) + 2)
	unionHostileContent = append(unionHostileContent, byte(ir.TableKindI32), 4, 0, 0, 0, 0)
	unionHostile := &wire32Builder{}
	unionHostile.intern(fieldID("UnionRoot"))
	unionHostile.intern(armID)
	unionHostile.body = append(unionHostile.body, 1, byte(ir.TableKindUnion))
	unionHostile.body = append(unionHostile.body, unionHostileContent...)
	cases = append(cases, wire32Case{"union/reference", "UnionRoot", unionHostile.file(), false})

	// A framed length past the bytes left is refused by both builds. The
	// declared 2^32 bytes narrow to none; the u64 comparison sees them.
	unknownLength := &wire32Builder{}
	unknownLength.field(unknown, byte(ir.TableKindString), leb32(uint64(1)<<32))
	cases = append(cases, wire32Case{"unknown/length", "Plain", unknownLength.file(), false})

	// A list count of 2^32 + k, past every element the body can hold.
	list := &wire32Builder{}
	listContent := []byte{byte(ir.TableKindI32)}
	listContent = append(listContent, leb32((uint64(1)<<32)+1)...)
	list.field(fieldID("ListRoot"), byte(ir.TableKindArray), append(leb32(uint64(len(listContent))), listContent...))
	cases = append(cases, wire32Case{"list/length", "ListRoot", list.file(), false})

	return cases
}

// rust32BitTarget returns the first 32-bit target triple rustc has a standard
// library for, or fails/skips naming the missing fact.
func rust32BitTarget(t *testing.T) string {
	t.Helper()
	for _, triple := range []string{
		"i686-unknown-linux-gnu",
		"i686-unknown-linux-musl",
		"armv7-unknown-linux-gnueabihf",
	} {
		out, err := exec.Command("rustc", "--print", "target-libdir", "--target", triple).Output()
		if err != nil {
			continue
		}
		libs, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(out)), "libstd-*.rlib"))
		if len(libs) > 0 {
			return triple
		}
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc has no 32-bit target with std installed, so the wire lengths and references cannot be compared across usize widths")
	}
	t.Skipf("rustc has no 32-bit target with std installed and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that builds and runs Rust for a 32-bit target")
	return ""
}

// writeWire32Probe lays the generated unit out as one flat crate and writes the
// main that prints one verdict per case.
func writeWire32Probe(t *testing.T, u *ir.Unit, cases []wire32Case) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" || name == TableEnumsModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs") ||
			strings.HasSuffix(name, "_table.rs") || strings.HasSuffix(name, "_variable.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables, unused_mut)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString("\nfn main() {\n")
	for _, c := range cases {
		fmt.Fprintf(&main, `    {
        let wire: &[u8] = &[%s];
        let mut report = TableReport::default();
        let accepted = %s_load(wire, &mut report).is_some();
        println!("WIRE {} {}", %q, if accepted { "accepted" } else { "refused" });
    }
`, rustBytes(c.wire), ir.RustSnake(c.root), c.name)
	}
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func buildWire32Probe(t *testing.T, dir, target, bin string) error {
	t.Helper()
	args := []string{"--edition", "2024", "-O", "-A", "warnings"}
	if cc, err := exec.LookPath("cc"); err == nil {
		if abs, err := filepath.Abs(cc); err == nil {
			cc = abs
		}
		args = append(args, "-C", "linker="+cc)
	}
	if target != "" {
		args = append(args, "--target", target)
	}
	args = append(args, "-o", bin, "main.rs")
	build := exec.Command("rustc", args...)
	build.Dir = dir
	out, err := build.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v\n%s", err, out)
	}
	return nil
}

// targetCommand runs a binary built for target under the runner cargo names for
// that target (CARGO_TARGET_<TRIPLE>_RUNNER, for example qemu-i386), and
// directly when the variable is unset.
func targetCommand(target, bin string) *exec.Cmd {
	key := "CARGO_TARGET_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(target)) + "_RUNNER"
	runner := strings.Fields(os.Getenv(key))
	if len(runner) == 0 {
		return exec.Command(bin)
	}
	return exec.Command(runner[0], append(runner[1:], bin)...)
}

func wire32Verdicts(out []byte) map[string]bool {
	verdicts := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "WIRE" {
			verdicts[fields[1]] = fields[2] == "accepted"
		}
	}
	return verdicts
}

func runWire32Probe(t *testing.T, bin, dir string) map[string]bool {
	t.Helper()
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	return wire32Verdicts(out)
}

func TestRustWireLengthsOn32BitTargets(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust for a 32-bit target)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the wire lengths cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	u := unitFrom(t, wireLengths32BitSchema)
	model := tabletext.NewModel(u)
	cases := wire32Cases(u)
	for _, c := range cases {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(model.Lookup(c.root)), c.wire, &report)
		if ok != c.want {
			t.Fatalf("%s: the engine says %v (%v), want %v\n%x", c.name, ok, err, c.want, c.wire)
		}
	}

	dir := writeWire32Probe(t, u, cases)

	hostBin := filepath.Join(t.TempDir(), "wire32-host")
	if err := buildWire32Probe(t, dir, "", hostBin); err != nil {
		t.Fatalf("the host probe did not build: %v", err)
	}
	host := runWire32Probe(t, hostBin, dir)

	target := rust32BitTarget(t)
	targetBin := filepath.Join(t.TempDir(), "wire32-target")
	if err := buildWire32Probe(t, dir, target, targetBin); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the probe did not build for the 32-bit target %s: %v", target, err)
		}
		t.Skipf("the probe did not build for the 32-bit target %s and SCHEMA_REQUIRE_CORPUS is unset: %v", target, err)
	}
	run := targetCommand(target, targetBin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the probe did not run for the 32-bit target %s (%s): %v\n%s", target, run.String(), err, out)
		}
		t.Skipf("the probe did not run for the 32-bit target %s and SCHEMA_REQUIRE_CORPUS is unset: %v\n%s", target, err, out)
	}
	wide := wire32Verdicts(out)

	for _, c := range cases {
		h, okHost := host[c.name]
		n, okTarget := wide[c.name]
		if !okHost || !okTarget {
			t.Fatalf("%s: the probe reported no verdict (host %t, 32-bit %t)", c.name, okHost, okTarget)
		}
		if h != c.want || n != c.want {
			t.Errorf("%s: the engine says %v, the 64-bit build says %v, the 32-bit build says %v", c.name, c.want, h, n)
		}
		if h != n {
			t.Errorf("%s: the 32-bit build says %v where the 64-bit build says %v", c.name, n, h)
		}
	}
}
