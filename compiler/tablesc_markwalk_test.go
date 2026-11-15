package compiler

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The C reachability walk runs before the validating read and must cost time
// bounded by the bytes it is given (docs/SPEC-TABLES.md §3.1, §4): an array
// count the body cannot cover, an element that consumes nothing, and a framed
// payload past the nesting bound all stop the walk instead of driving it. The
// wire below is built in Go so the probe is a wall-clock Functional test: each
// case must finish well inside the deadline, and the root-body case must also
// refuse.
const markWalkSchema = `package probe

table Node
{
    x int32 = 0
}

table Root
{
    head *Node
}
`

func markWalkLeb(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func markWalkRawField(ref uint64, kind byte, payload []byte) []byte {
	out := markWalkLeb(nil, ref)
	out = append(out, kind)
	return append(out, payload...)
}

func markWalkFramedField(ref uint64, kind byte, content []byte) []byte {
	out := markWalkRawField(ref, kind, markWalkLeb(nil, uint64(len(content))))
	return append(out, content...)
}

func markWalkNodePayload(typeRef uint64, body []byte) []byte {
	out := markWalkLeb(nil, 1)
	out = markWalkLeb(out, typeRef)
	out = markWalkLeb(out, uint64(len(body)))
	return append(out, body...)
}

func markWalkWire(body []byte, ids []uint64) []byte {
	out := []byte{ir.TableWireForm}
	out = append(out, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func markWalkHugeArray() []byte {
	return markWalkLeb([]byte{byte(ir.TableKindI32)}, uint64(1)<<63)
}

func markWalkRootBody() []byte {
	ids := []uint64{
		ir.TableWireId("head"),
		ir.TableWireId("markwalk-unknown-array"),
		ir.TableNodeWireId,
		ir.TableWireId("Node"),
	}
	body := markWalkRawField(1, byte(ir.TableKindPointer), markWalkLeb(nil, 2))
	body = append(body, markWalkFramedField(2, byte(ir.TableKindArray), markWalkHugeArray())...)
	body = append(body, markWalkFramedField(3, byte(ir.TableKindString), markWalkNodePayload(4, []byte{0}))...)
	body = append(body, 0)
	return markWalkWire(body, ids)
}

func markWalkUnnamedRecord() []byte {
	const (
		fieldRef = 1
		nodeRef  = 2
		typeRef  = 3
	)
	ids := []uint64{ir.TableWireId("markwalk-unseen-field"), ir.TableNodeWireId, ir.TableWireId("markwalk-unseen-record")}
	record := markWalkFramedField(fieldRef, byte(ir.TableKindArray), markWalkHugeArray())
	record = append(record, 0)
	body := markWalkRawField(fieldRef, byte(ir.TableKindPointer), markWalkLeb(nil, 2))
	body = append(body, markWalkFramedField(nodeRef, byte(ir.TableKindString), markWalkNodePayload(typeRef, record))...)
	body = append(body, 0)
	return markWalkWire(body, ids)
}

func markWalkNestedKeyed(levels int) []byte {
	inner := []byte{byte(ir.TableKindI32), 0}
	for range levels {
		payload := []byte{byte(ir.TableKindKeyed)}
		payload = markWalkLeb(payload, 1)
		payload = markWalkLeb(payload, 1)
		payload = markWalkLeb(payload, uint64(len(inner)))
		payload = append(payload, inner...)
		inner = payload
	}
	ids := []uint64{
		ir.TableWireId("head"),
		ir.TableWireId("markwalk-nested-keyed"),
		ir.TableNodeWireId,
		ir.TableWireId("Node"),
	}
	body := markWalkRawField(1, byte(ir.TableKindPointer), markWalkLeb(nil, 2))
	body = append(body, markWalkFramedField(2, byte(ir.TableKindKeyed), inner)...)
	body = append(body, markWalkFramedField(3, byte(ir.TableKindString), markWalkNodePayload(4, []byte{0}))...)
	body = append(body, 0)
	return markWalkWire(body, ids)
}

func compileCTableMarkWalkProbe(t *testing.T, u *ir.Unit, source string) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
	files, err := generateCUnqualified(u)
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
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c")}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	return filepath.Join(dir, "probe")
}

func runCTableMarkWalkProbe(t *testing.T, binary, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, name).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("the C probe for %s did not finish within 20s\n%s", name, output)
	}
	if err != nil {
		t.Fatalf("the C probe for %s failed: %v\n%s", name, err, output)
	}
}

func TestFunctionalCTableReachWalkBoundedByBody(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	u := unitFromSource(t, markWalkSchema)

	rootBody := markWalkRootBody()
	unnamedRecord := markWalkUnnamedRecord()
	nestedKeyed := markWalkNestedKeyed(129)

	model := tabletext.NewModel(u)
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(u.Tables["Root"]), rootBody, &report); ok || !report.Refused {
		t.Fatalf("the Go engine read a root body whose array count the body cannot cover: ok=%v report=%+v err=%v", ok, report, err)
	}

	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t root_body[]={%s};
static const uint8_t unnamed_record[]={%s};
static const uint8_t nested_keyed[]={%s};
static int load_wire(const uint8_t * wire, int64_t bytes, int want_refused)
{
    TableReport report;
    const Root * root;
    uint8_t * region;
    int64_t need = root_load_measure(wire, bytes);
    CHECK(need > 0);
    region = (uint8_t *) malloc((size_t) need);
    CHECK(region != NULL);
    memset(&report, 0, sizeof(report));
    root = root_load(region, need, wire, bytes, &report);
    if (want_refused) { CHECK(root == NULL); CHECK(report.refused); }
    free(region);
    return 0;
}
int main(int argc, char ** argv)
{
    if (argc < 2) { return 2; }
    if (strcmp(argv[1], "root-body") == 0) { return load_wire(root_body, (int64_t) sizeof(root_body), 1); }
    if (strcmp(argv[1], "unnamed-record") == 0) { return load_wire(unnamed_record, (int64_t) sizeof(unnamed_record), 0); }
    if (strcmp(argv[1], "nested-keyed") == 0) { return load_wire(nested_keyed, (int64_t) sizeof(nested_keyed), 0); }
    return 2;
}
`, cppWire(rootBody), cppWire(unnamedRecord), cppWire(nestedKeyed))
	binary := compileCTableMarkWalkProbe(t, u, source)
	for _, name := range []string{"root-body", "unnamed-record", "nested-keyed"} {
		t.Run(name, func(t *testing.T) {
			runCTableMarkWalkProbe(t, binary, name)
		})
	}
}
