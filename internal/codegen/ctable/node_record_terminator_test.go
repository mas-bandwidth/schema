// C framing: a node record's terminator is the end of its payload
// (docs/SPEC-TABLES.md §3, §3.1; docs/LAYERS/1-framing.md L3). A record body
// that reaches its terminator with a further byte still inside the record's L
// is damage, and the strict reads refuse it: the region read and the builder
// read. A clean record whose terminator is its last byte still reads.
package ctable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cNodeRecordTerminatorSchema = `package graphdemo
table ListNode
{
    value int32
    name  string(12)
    next  *ListNode
}
`

// cNodeRecordTerminatorWire is the framing vector's byte script: the root
// body names a record of `ListNode { value = 2 }`, and with extra the
// record's L holds the terminator and one byte after it. The id table is in
// first-use order with every entry used (docs/SPEC-TABLES.md §3): value,
// next, the reserved node-table id, and the record's type id. The empty name
// rides nowhere, so it names no entry.
func cNodeRecordTerminatorWire(t *testing.T, u *ir.Unit, extra bool) []byte {
	t.Helper()
	ids := []uint64{
		ir.TableWireId("value"),
		ir.TableWireId("next"),
		ir.TableNodeWireId,
		ir.TableWireId(u.Tables["ListNode"].WireName()),
	}
	record := []byte{4, 0, 1, byte(ir.TableKindI32)}
	record = binary.LittleEndian.AppendUint32(record, 2)
	record = append(record, 0)
	if extra {
		record = append(record, 0xFF)
	}
	record[1] = byte(len(record) - 2)
	nodeTable := append([]byte{1}, record...)
	var body []byte
	body = append(body, 1, byte(ir.TableKindI32))
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = append(body, 2, byte(ir.TableKindPointer), 2)
	body = append(body, 3, byte(ir.TableKindString), byte(len(nodeTable)))
	body = append(body, nodeTable...)
	body = append(body, 0)
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

// runCNodeRecordTerminatorProbe compiles the generated C and the given probe
// body; mutate, when set, edits the generated sources before they are built.
func runCNodeRecordTerminatorProbe(t *testing.T, u *ir.Unit, body string, mutate func(map[string][]byte) error) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if mutate != nil {
		if err := mutate(files); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(void) {
` + body + `
    return 0;
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return string(output), fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "probe"))
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestCNodeRecordEndsAtTerminator(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	u := unitFrom(t, cNodeRecordTerminatorSchema)
	body := fmt.Sprintf(`
    int failures = 0;
    static uint8_t region[65536] __attribute__((aligned(64)));
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %%s (line %%d)\n", (name), __LINE__); failures++; } } while (0)
    {
        static const uint8_t clean[] = { %s };
        static const uint8_t tail[] = { %s };
        TableReport report; const ListNode * root; ListNodeBuilder builder; int ok;

        memset(region, 0, sizeof(region)); memset(&report, 0, sizeof(report));
        root = list_node_load(region, (int64_t)sizeof(region), clean, (int64_t)sizeof(clean), &report);
        CHECK(root != NULL && !report.refused, "clean: the region read keeps a record that ends at its terminator");

        CHECK(list_node_builder_init(&builder), "clean: builder init");
        memset(&report, 0, sizeof(report));
        ok = list_node_load_builder(&builder, clean, (int64_t)sizeof(clean), &report);
        CHECK(ok && !report.refused, "clean: the builder read keeps a record that ends at its terminator");
        list_node_builder_shutdown(&builder);

        memset(region, 0, sizeof(region)); memset(&report, 0, sizeof(report));
        root = list_node_load(region, (int64_t)sizeof(region), tail, (int64_t)sizeof(tail), &report);
        CHECK(root == NULL, "tail: the region read refuses bytes after the record terminator");
        CHECK(report.refused, "tail: the region read's report refuses");

        CHECK(list_node_builder_init(&builder), "tail: builder init");
        memset(&report, 0, sizeof(report));
        ok = list_node_load_builder(&builder, tail, (int64_t)sizeof(tail), &report);
        CHECK(!ok, "tail: the builder read refuses bytes after the record terminator");
        CHECK(report.refused, "tail: the builder read's report refuses");
        list_node_builder_shutdown(&builder);
    }
    return failures != 0;
`, cBytes(cNodeRecordTerminatorWire(t, u, false)), cBytes(cNodeRecordTerminatorWire(t, u, true)))
	output, err := runCNodeRecordTerminatorProbe(t, u, body, nil)
	if err != nil {
		t.Fatalf("node record terminator probe: %v\n%s", err, output)
	}
}

func TestCNodeRecordEndsAtTerminatorNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	u := unitFrom(t, cNodeRecordTerminatorSchema)
	body := fmt.Sprintf(`
    int failures = 0;
    static uint8_t region[65536] __attribute__((aligned(64)));
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %%s (line %%d)\n", (name), __LINE__); failures++; } } while (0)
    {
        static const uint8_t tail[] = { %s };
        TableReport report; const ListNode * root; ListNodeBuilder builder; int ok;

        memset(region, 0, sizeof(region)); memset(&report, 0, sizeof(report));
        root = list_node_load(region, (int64_t)sizeof(region), tail, (int64_t)sizeof(tail), &report);
        CHECK(root != NULL && !report.refused, "sabotaged: the region read accepts bytes after the terminator");

        CHECK(list_node_builder_init(&builder), "sabotaged: builder init");
        memset(&report, 0, sizeof(report));
        ok = list_node_load_builder(&builder, tail, (int64_t)sizeof(tail), &report);
        CHECK(ok && !report.refused, "sabotaged: the builder read accepts bytes after the terminator");
        list_node_builder_shutdown(&builder);
    }
    return failures != 0;
`, cBytes(cNodeRecordTerminatorWire(t, u, true)))
	mutate := func(files map[string][]byte) error {
		const check = "if(body.offset!=body.size)"
		hit := false
		for name, data := range files {
			if bytes.Contains(data, []byte(check)) {
				files[name] = bytes.ReplaceAll(data, []byte(check), []byte("if(0)"))
				hit = true
			}
		}
		if !hit {
			return fmt.Errorf("the generated C carries no record-whole check to remove")
		}
		return nil
	}
	output, err := runCNodeRecordTerminatorProbe(t, u, body, mutate)
	if err != nil {
		t.Fatalf("node record terminator negative control: %v\n%s", err, output)
	}
}
