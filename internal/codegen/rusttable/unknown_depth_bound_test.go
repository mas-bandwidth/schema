package rusttable

// THE BINARY NESTING BOUND ON THE RUST PLAIN WALK (docs/SPEC-TABLES.md §4,
// binary_max_depth): the default strict <root>_load walks an unknown field by
// the same one bound every reader and every path holds — depth 128 passes and
// 129 refuses — inside a table body, an array body, a keyed or map body, and a
// union whose arm is structural.
//
// A FUNCTIONAL TEST: it compiles and runs the generated Rust with rustc. It
// fails by name when SCHEMA_REQUIRE_CORPUS is set and rustc is absent, skips
// by name otherwise, and is gated by slowtest.Gate (SCHEMA_SLOW=1 runs it).
// The same two files go to the engine (internal/tablewire) for a verdict, so
// this test compares the leg with the oracle instead of pinning an answer.

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

const rustUnknownDepthSchema = `package depthprobe

table Root
{
    z int32
}
`

const rustUnknownDepthID = uint64(0x1234567890abcdef)

func depthUvarint(v uint64) []byte { return binary.AppendUvarint(nil, v) }

// depthFramed is one field carrying a length-framed body: the unknown field
// reference, the kind, its L, and the body.
func depthFramed(kind byte, content []byte) []byte {
	out := []byte{1, kind}
	out = append(out, depthUvarint(uint64(len(content)))...)
	return append(out, content...)
}

func depthBody(field []byte) []byte { return append(field, 0) }

// depthFile closes a root body with its id table: the one unknown id.
func depthFile(body []byte) []byte {
	out := []byte{byte(ir.TableWireForm)}
	out = append(out, body...)
	out = binary.LittleEndian.AppendUint64(out, rustUnknownDepthID)
	return binary.LittleEndian.AppendUint64(out, 1)
}

// depthNestedTable nests `levels` table bodies inside one unknown field.
func depthNestedTable(levels int) []byte {
	inner := []byte{0}
	kind := byte(ir.TableKindTable)
	for i := 1; i < levels; i++ {
		inner = depthBody(depthFramed(kind, inner))
		kind = byte(ir.TableKindTable)
	}
	return depthFile(depthBody(depthFramed(kind, inner)))
}

// depthNestedArray puts an array body at the deepest level, under table bodies.
func depthNestedArray(levels int) []byte {
	inner := []byte{byte(ir.TableKindI32), 1, 7, 0, 0, 0}
	kind := byte(ir.TableKindArray)
	for i := 1; i < levels; i++ {
		inner = depthBody(depthFramed(kind, inner))
		kind = byte(ir.TableKindTable)
	}
	return depthFile(depthBody(depthFramed(kind, inner)))
}

// depthNestedKeyed puts a keyed body at the deepest level, under table bodies.
func depthNestedKeyed(levels int) []byte {
	inner := []byte{byte(ir.TableKindI32), 0}
	kind := byte(ir.TableKindKeyed)
	for i := 1; i < levels; i++ {
		inner = depthBody(depthFramed(kind, inner))
		kind = byte(ir.TableKindTable)
	}
	return depthFile(depthBody(depthFramed(kind, inner)))
}

// depthNestedUnion nests `levels` union payloads, each with a structural arm,
// ending in an empty union (arm reference 0).
func depthNestedUnion(levels int) []byte {
	payload := []byte{0}
	for range levels {
		out := []byte{1, byte(ir.TableKindUnion)}
		out = append(out, depthUvarint(uint64(len(payload)))...)
		out = append(out, payload...)
		payload = out
	}
	root := []byte{1, byte(ir.TableKindUnion)}
	root = append(root, payload...)
	return depthFile(depthBody(root))
}

func TestRustUnknownFieldBinaryDepthBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (the generated plain load's unknown-field walk)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the unknown-field depth bound cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	u := unitFrom(t, rustUnknownDepthSchema)
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	load := ir.RustSnake("Root") + "_load"

	shapes := []struct {
		name  string
		build func(int) []byte
	}{
		{"table-body", depthNestedTable},
		{"array-body", depthNestedArray},
		{"keyed-body", depthNestedKeyed},
		{"union-structural-arm", depthNestedUnion},
	}

	var body strings.Builder
	body.WriteString("    let mut failures = 0;\n")
	for _, shape := range shapes {
		for _, levels := range []int{ir.TableBinaryMaxDepth, ir.TableBinaryMaxDepth + 1} {
			wire := shape.build(levels)
			var report tabletext.Report
			ok, err := tablewire.Decode(m, m.New(root), wire, &report)
			want := levels == ir.TableBinaryMaxDepth
			if ok != want || (ok && report.Unknown != 1) {
				t.Fatalf("%s/%d: the engine answered ok=%v err=%v unknown=%d, want ok=%v unknown=1",
					shape.name, levels, ok, err, report.Unknown, want)
			}
			fmt.Fprintf(&body, `
    {
        let wire: &[u8] = &[%s];
        let mut report = TableReport::default();
        let got = %s(wire, &mut report);
        if got.is_some() != %t {
            eprintln!("FAIL %s/%d: the load answered {}, the engine {}", got.is_some(), %t);
            failures += 1;
        }
        if %t && report.unknown != 1 {
            eprintln!("FAIL %s/%d: the load accepted with unknown={}", report.unknown);
            failures += 1;
        }
    }
`, rustBytes(wire), load, want, shape.name, levels, want, want, shape.name, levels)
		}
	}
	body.WriteString("    if failures > 0 { std::process::exit(1); }\n")

	out, err := runUnknownDepthProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	if strings.Contains(out, "FAIL") {
		t.Fatalf("the plain load disagrees with the engine at the binary bound:\n%s", out)
	}
}

// runUnknownDepthProbe builds the unit's wire modules beside a main that runs
// body, with rustc alone: the table runtimes are self-contained.
func runUnknownDepthProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
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
	main.WriteString(body)
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	build.Dir = dir
	out, err := build.CombinedOutput()
	if err != nil && strings.Contains(string(out), "could not exec the linker") {
		// A sandbox that denies execvp of a bare linker name still reaches an
		// absolute one; a normal host takes the plain build above.
		if _, statErr := os.Stat("/usr/bin/gcc"); statErr == nil {
			build = exec.Command("rustc", "--edition", "2024", "-C", "linker=/usr/bin/gcc", "-O", "-o", bin, "main.rs")
			build.Dir = dir
			out, err = build.CombinedOutput()
		}
	}
	if err != nil {
		return string(out), err
	}
	run := exec.Command(bin)
	run.Dir = dir
	got, err := run.CombinedOutput()
	return string(got), err
}
