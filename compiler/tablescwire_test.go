package compiler

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Exercise generated code across the first-use reference-width boundary. The
// expectation is produced by the independent compiler engine, not the emitter.
// Nested frames must measure with the vocabulary already accumulated by their
// parent, and repeated saves must not retain a previous call's vocabulary.
func TestCTableWireReferenceBoundary(t *testing.T) {
	var schema, fill strings.Builder
	schema.WriteString(`package probe

enum Choice {
Current
Second
}
type Leaf { choice Choice }
table Root {
`)
	for i := range 127 {
		fmt.Fprintf(&schema, "field%d uint8\n", i)
		fmt.Fprintf(&fill, "    value.field%d = 1;\n", i)
	}
	schema.WriteString("leaf Leaf | was = \"oldLeaf\"\nleaves [..2]Leaf\nslots [Choice]Leaf\n}\n")
	fill.WriteString("    value.leaf.choice = CHOICE_CURRENT;\n    value.leaves_count = 2;\n    value.leaves[0].choice = CHOICE_SECOND;\n    value.leaves[1].choice = CHOICE_CURRENT;\n    value.slots[0].choice = CHOICE_SECOND;\n")
	u := unitFromSource(t, schema.String())
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	for i := range 127 {
		value.Fields[i].Cell.U = 1
	}
	value.Fields[127].Cell.Tab.Fields[0].Cell.U = 1
	value.Fields[128].Count = 2
	value.Fields[128].Elems[0].Tab.Fields[0].Cell.U = 2
	value.Fields[128].Elems[1].Tab.Fields[0].Cell.U = 1
	value.Fields[129].Elems[0].Tab.Fields[0].Cell.U = 2
	expected, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	if count := binary.LittleEndian.Uint64(expected[len(expected)-8:]); count <= 128 {
		t.Fatalf("test did not cross the identity boundary: %d", count)
	}
	var octets strings.Builder
	for _, b := range expected {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t expected[] = { %s };
int main(void)
{
    Root value, decoded;
    TableReport report;
    uint8_t saved[sizeof(expected) + 1], damaged[sizeof(expected)];
    int64_t n;
    root_reset(&value);
%s
    n = root_measure(&value);
    CHECK(n == (int64_t)sizeof(expected));
    CHECK(root_save(&value, saved, n) == n);
    CHECK(memcmp(saved, expected, sizeof(expected)) == 0);
    CHECK(root_save(&value, saved, n) == n);
    CHECK(memcmp(saved, expected, sizeof(expected)) == 0);
    saved[n-1] = 0x7d;
    CHECK(root_save(&value, saved, n-1) == -1);
    CHECK(saved[n-1] == 0x7d);
    CHECK(root_save(&value, saved, -1) == -1);
    CHECK(root_save(&value, NULL, n) == -1);
    memset(&report, 0, sizeof(report));
    CHECK(root_load(&decoded, expected, n, &report));
    CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.widened && !report.clamped);
    CHECK(root_save(&decoded, saved, n) == n);
    CHECK(memcmp(saved, expected, sizeof(expected)) == 0);
    CHECK(root_load(&decoded, expected, n, NULL));
    memcpy(damaged, expected, sizeof(expected));
    damaged[0] = 2;
    memset(&report, 0, sizeof(report));
    CHECK(!root_load(&decoded, damaged, n, &report));
    CHECK(report.refused && report.reason == SCHEMA_TABLE_MESSAGE_FORM_AS_FILE && !report.malformed);
    CHECK(root_measure(&decoded) == 10);
    damaged[0] = 99;
    memset(&report, 0, sizeof(report));
    CHECK(!root_load(&decoded, damaged, n, &report));
    CHECK(report.refused && report.reason == SCHEMA_TABLE_NEWER_FORM && !report.malformed);
    memset(&report, 0, sizeof(report));
    CHECK(!root_load(&decoded, NULL, n, &report) && report.malformed && !report.refused);
    return 0;
}
`, octets.String(), fill.String())
	runCTableWireProbe(t, u, source)
}

func runCTableWireProbe(t *testing.T, u *ir.Unit, source string) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
	files, err := New().Generate(u, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c"), filepath.Join(dir, "ProbeTable.c"), "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
}

// Clamps must happen after promoting the source-width integer. NaN widening
// must preserve its sign and payload without quieting a signaling NaN.
func TestCTableWireWidening(t *testing.T) {
	u := unitFromSource(t, `package probe
    table Root {
        low int32 = 1000 | min = 1000, max = 2000
        high int32 = -1000 | min = -2000, max = -1000
        positive uint64 = 1000 | min = 1000, max = 2000
        real float64
        samples [..2]uint32 | min = 0, max = 2000
    }
    `)
	wire := []byte{1, 1, 2, 255, 2, 2, 0, 3, 6, 255, 4, 10}
	wire = binary.LittleEndian.AppendUint32(wire, 0xffa5a5a5)
	wire = append(wire, 5, 14, 6, 7, 2, 0, 0, 255, 255, 0)
	for _, f := range u.Tables["Root"].Fields {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
	}
	wire = binary.LittleEndian.AppendUint64(wire, 5)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, value, wire, &report); err != nil || !ok {
		t.Fatalf("oracle decode: %v %v", ok, err)
	}
	if report.Widened != 5 || report.Clamped != 4 || report.Malformed {
		t.Fatalf("test did not exercise five widenings and four clamps: %+v", report)
	}
	if value.Fields[2].Cell.U != 1000 {
		t.Fatalf("oracle lost the widened clamp: %d", value.Fields[2].Cell.U)
	}
	saved, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	octets := func(data []byte) string {
		var s strings.Builder
		for _, b := range data {
			fmt.Fprintf(&s, "0x%02x,", b)
		}
		return s.String()
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
    #include <stdio.h>
    static const uint8_t wire[] = { %s }, expected[] = { %s };
    int main(void) {
        Root value; TableReport report; uint8_t saved[sizeof(expected)];
        memset(&report, 0, sizeof(report));
        if (!root_load(&value, wire, sizeof(wire), &report)) return 1;
        if (report.widened != 5 || report.clamped != 4 || report.malformed || report.refused) return 2;
        if (root_measure(&value) != (int64_t)sizeof(expected)) { fprintf(stderr, "measure %%lld expected %%zu: low %%d high %%d positive %%llu samples %%d [%%u, %%u]\n", (long long)root_measure(&value), sizeof(expected), value.low, value.high, (unsigned long long)value.positive, value.samples_count, value.samples[0], value.samples[1]); return 3; }
        if (root_save(&value, saved, sizeof(saved)) != (int64_t)sizeof(expected)) return 4;
        return memcmp(saved, expected, sizeof(expected)) != 0;
    }
    `, octets(wire), octets(saved)))
}
