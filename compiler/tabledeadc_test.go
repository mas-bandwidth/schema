package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Card C-6: Stop emitting check_default where no probe can set it.
//
// A TableWriter probe sets check_default = 1 to test whether a nested table's
// body elides on the wire (returns offset 1 / bit 1 if all fields match their
// defaults, or offset 2 / bit 2 early exit on the first non-default field).
//
// In units where no table is nested by value or keyed array, no probe can ever
// set check_default. In such units:
//   - TableWriter and TableBitWriter omit check_default entirely;
//   - table_retain_tail omits if ( w->check_default );
//   - scalar leaf measure omits && !w->check_default;
//   - the string "check_default" is completely absent from the emitted header and source.
//
// In units with probes:
//   - TableWriter and TableBitWriter carry check_default;
//   - only structs that can be reached by a probe (targets of scalar table fields
//     or enum-keyed table arrays) emit check_default early-return checks;
//   - unprobed structs (such as root tables or tables only appearing in lists/unions)
//     do not emit unreachable check_default tests.

const deadCUnprobedSrc = `package probe

fixed table ScalarLeaf
{
    id    int32
    scale float32 = 1.0
    tag   string(16)
}

fixed table RootTable
{
    count  int32
    scores [4]int32
}
`

const deadCProbedSrc = `package probe

fixed table Leaf
{
    x int32 = 0
    y int32 = 0
}

fixed table Outer
{
    id   int32
    leaf Leaf
}
`

func compileAndRunC(t *testing.T, u *ir.Unit, mainSource string) {
	t.Helper()
	slowtest.Gate(t, "the C compiler (cc)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
	files, err := New().Generate(u, "c", Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(mainSource), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c")}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	cmd := exec.Command(cc, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	if output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
}

// TestCTableDeadCheckDefault_Unprobed verifies that a unit without nested table
// probes emits zero check_default symbols across both header and source,
// and that the resulting codecs compile and execute cleanly.
func TestCTableDeadCheckDefault_Unprobed(t *testing.T) {
	u := unitFromSource(t, deadCUnprobedSrc)
	files, err := New().Generate(u, "c", Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	header := string(files["ProbeTable.h"])
	source := string(files["ProbeTable.c"])

	// 1. check_default must NOT appear anywhere in the generated header or source.
	if strings.Contains(header, "check_default") {
		t.Errorf("unprobed unit header contains dead check_default symbol")
	}
	if strings.Contains(source, "check_default") {
		t.Errorf("unprobed unit source contains dead check_default symbol")
	}

	// 2. TableWriter and TableBitWriter must omit check_default.
	if !strings.Contains(header, "int overflow, id_checkpoint;") {
		t.Errorf("expected TableWriter without check_default, got header:\n%s", header)
	}
	if !strings.Contains(header, "int overflow;") {
		t.Errorf("expected TableBitWriter without check_default, got header:\n%s", header)
	}

	// 3. Scalar leaf measure must not have && !w->check_default.
	if strings.Contains(header, "!w->check_default") {
		t.Errorf("scalar leaf measure still contains dead !w->check_default")
	}
	if !strings.Contains(header, "if ( w->buffer == NULL )") {
		t.Errorf("expected scalar leaf measure with 'if ( w->buffer == NULL )'")
	}

	// 4. Compile and verify roundtrip execution.
	const driver = `#include <stdio.h>
#include <string.h>
#include <assert.h>
#include "ProbeTable.h"

int main(void) {
    uint8_t buf[512];

    /* Test RootTable */
    RootTable root;
    root_table_reset(&root);
    root.count = 42;
    root.scores[0] = 10;
    root.scores[1] = 20;

    int64_t measured = root_table_measure(&root);
    assert(measured > 0);
    int64_t saved = root_table_save(&root, buf, sizeof(buf));
    assert(saved == measured);

    RootTable loaded;
    root_table_reset(&loaded);
    TableReport report;
    memset(&report, 0, sizeof(report));
    assert(root_table_load(&loaded, buf, saved, &report) == 1);
    assert(report.malformed == 0);
    assert(loaded.count == 42);
    assert(loaded.scores[0] == 10);
    assert(loaded.scores[1] == 20);

    /* Test ScalarLeaf */
    ScalarLeaf leaf;
    scalar_leaf_reset(&leaf);
    leaf.id = 7;
    leaf.scale = 2.5f;
    leaf.tag_length = 4;
    memcpy(leaf.tag, "test", 4);

    measured = scalar_leaf_measure(&leaf);
    assert(measured > 0);
    saved = scalar_leaf_save(&leaf, buf, sizeof(buf));
    assert(saved == measured);

    ScalarLeaf loaded_leaf;
    scalar_leaf_reset(&loaded_leaf);
    memset(&report, 0, sizeof(report));
    assert(scalar_leaf_load(&loaded_leaf, buf, saved, &report) == 1);
    assert(report.malformed == 0);
    assert(loaded_leaf.id == 7);
    assert(loaded_leaf.scale == 2.5f);
    assert(loaded_leaf.tag_length == 4);
    assert(memcmp(loaded_leaf.tag, "test", 4) == 0);

    return 0;
}
`
	compileAndRunC(t, u, driver)
}

// TestCTableDeadCheckDefault_Probed verifies that in a unit with nested table
// probes, check_default is emitted only on probeable structs (Leaf) and on the
// writer structs, and omitted from unprobeable structs (Outer).
func TestCTableDeadCheckDefault_Probed(t *testing.T) {
	u := unitFromSource(t, deadCProbedSrc)
	files, err := New().Generate(u, "c", Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	header := string(files["ProbeTable.h"])

	// 1. TableWriter and TableBitWriter must carry check_default.
	if !strings.Contains(header, "int overflow, id_checkpoint, check_default;") {
		t.Errorf("expected TableWriter with check_default")
	}
	if !strings.Contains(header, "int overflow,check_default;") {
		t.Errorf("expected TableBitWriter with check_default")
	}

	// 2. Leaf (probed by Outer) must emit check_default early-return checks.
	defLeaf := "int leaf_save_body( TableWriter * w, const Leaf * value )\n{"
	defOuter := "int outer_save_body( TableWriter * w, const Outer * value )\n{"
	leafIdx := strings.Index(header, defLeaf)
	outerIdx := strings.Index(header, defOuter)
	if leafIdx < 0 || outerIdx < 0 {
		t.Fatalf("could not find save_body definitions in header (leafIdx=%d, outerIdx=%d)", leafIdx, outerIdx)
	}

	// Slice from the definition of each function to its closing return.
	leafBody := header[leafIdx:]
	if end := strings.Index(leafBody, "return !w->overflow;\n}"); end > 0 {
		leafBody = leafBody[:end]
	}
	outerBody := header[outerIdx:]
	if end := strings.Index(outerBody, "return !w->overflow;\n}"); end > 0 {
		outerBody = outerBody[:end]
	}

	// leaf_save_body CAN be reached by a probe: it MUST have check_default early exit.
	if !strings.Contains(leafBody, "if ( w->check_default ) { w->offset = 2; return 1; }") {
		t.Errorf("leaf_save_body is probed but missing check_default check:\n%s", leafBody)
	}
	if !strings.Contains(leafBody, "if ( w->buffer == NULL && !w->check_default )") {
		t.Errorf("leaf_save_body is probed but missing !w->check_default in scalar leaf measure:\n%s", leafBody)
	}

	// outer_save_body is NOT probed by anything: it MUST NOT have check_default early exit!
	if strings.Contains(outerBody, "if ( w->check_default )") {
		t.Errorf("outer_save_body is not probed but contains dead if ( w->check_default ):\n%s", outerBody)
	}

	// 3. Message save: leaf_save_message_body must have check_default; outer must not.
	defMsgLeaf := "int leaf_save_message_body(TableBitWriter * w,const Leaf * value)\n{"
	defMsgOuter := "int outer_save_message_body(TableBitWriter * w,const Outer * value)\n{"
	leafMsgIdx := strings.Index(header, defMsgLeaf)
	outerMsgIdx := strings.Index(header, defMsgOuter)
	if leafMsgIdx < 0 || outerMsgIdx < 0 {
		t.Fatalf("could not find save_message_body definitions in header (leafMsgIdx=%d, outerMsgIdx=%d)", leafMsgIdx, outerMsgIdx)
	}

	leafMsgBody := header[leafMsgIdx:]
	if end := strings.Index(leafMsgBody, "return !w->overflow;\n}"); end > 0 {
		leafMsgBody = leafMsgBody[:end]
	}
	outerMsgBody := header[outerMsgIdx:]
	if end := strings.Index(outerMsgBody, "return !w->overflow;\n}"); end > 0 {
		outerMsgBody = outerMsgBody[:end]
	}

	if !strings.Contains(leafMsgBody, "if(w->check_default)") {
		t.Errorf("leaf_save_message_body is probed but missing check_default check:\n%s", leafMsgBody)
	}
	if strings.Contains(outerMsgBody, "if(w->check_default)") {
		t.Errorf("outer_save_message_body is not probed but contains dead if(w->check_default):\n%s", outerMsgBody)
	}

	// 4. Compile and verify default elision and roundtrip execution.
	const driver = `#include <stdio.h>
#include <string.h>
#include <assert.h>
#include "ProbeTable.h"

int main(void) {
    uint8_t buf_def[512];
    uint8_t buf_nondef[512];

    /* Case 1: Outer with default Leaf {0, 0}.
       The default subtable MUST elide on the wire. */
    Outer outer_def;
    outer_reset(&outer_def);
    outer_def.id = 1;
    /* leaf.x and leaf.y are 0 (default) */

    int64_t measured_def = outer_measure(&outer_def);
    int64_t saved_def = outer_save(&outer_def, buf_def, sizeof(buf_def));
    assert(saved_def == measured_def);

    /* Case 2: Outer with non-default Leaf {42, 0}.
       The subtable MUST be emitted. */
    Outer outer_nondef;
    outer_reset(&outer_nondef);
    outer_nondef.id = 1;
    outer_nondef.leaf.x = 42;

    int64_t measured_nondef = outer_measure(&outer_nondef);
    int64_t saved_nondef = outer_save(&outer_nondef, buf_nondef, sizeof(buf_nondef));
    assert(saved_nondef == measured_nondef);

    /* The non-default leaf takes strictly more bytes than the elided default leaf. */
    assert(saved_nondef > saved_def);

    /* Verify decode of both */
    TableReport report;
    Outer loaded;

    /* Decode default outer */
    outer_reset(&loaded);
    memset(&report, 0, sizeof(report));
    assert(outer_load(&loaded, buf_def, saved_def, &report) == 1);
    assert(report.malformed == 0);
    assert(loaded.id == 1);
    assert(loaded.leaf.x == 0);
    assert(loaded.leaf.y == 0);

    /* Decode non-default outer */
    outer_reset(&loaded);
    memset(&report, 0, sizeof(report));
    assert(outer_load(&loaded, buf_nondef, saved_nondef, &report) == 1);
    assert(report.malformed == 0);
    assert(loaded.id == 1);
    assert(loaded.leaf.x == 42);
    assert(loaded.leaf.y == 0);

    return 0;
}
`
	compileAndRunC(t, u, driver)
}
