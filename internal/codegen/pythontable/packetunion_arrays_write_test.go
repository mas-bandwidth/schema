package pythontable

// THE WRITE REFUSALS of an array of packet unions on the Python leg
// (test/tables/packetunions; docs/SPEC-TABLES.md §2.6, §3, §3.3, §3.4, §16;
// SPEC.md §4.8). Python refuses a bad value in every build: each writer the
// root has (the variable form, the text form, and for the fixed root the fixed
// file and the message batch) raises InvalidValue for an array counting past
// its bound, an element whose tag names no arm, an arm whose packet field is
// outside its range, and a fixed array of the wrong length. The read side of
// the same unit is held by TestPythonVtUnionsRows (packet-union-arrays) and
// the conformance corpus.

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const packetUnionWriteProbe = `from tblpacketunions import InvalidValue, Shape, ShapeDoc, ShapeMsg, save_messages


def shape(tag, radius=0):
    s = Shape()
    s.type = tag
    s.ring.radius = radius
    return s


def writers(root):
    out = {"variable": lambda v: v.save(), "text": lambda v: v.to_json()}
    if root is ShapeMsg:
        out["fixed"] = lambda v: ShapeMsg.fixed_save([v])
        out["message"] = lambda v: save_messages(ShapeMsg, [v])
    return out


def count_past_bound(v):
    v.shapes = [shape(1, 1), shape(1, 2), shape(1, 3), shape(1, 4)]


def tag_past_max(v):
    v.shapes = [shape(3)]


def pair_tag_past_max(v):
    v.pair[0].type = 3


def arm_out_of_range(v):
    v.shapes = [shape(1, 65536)]


def pair_wrong_length(v):
    v.pair = [shape(0), shape(0), shape(0)]


faults = [count_past_bound, tag_past_max, pair_tag_past_max, arm_out_of_range, pair_wrong_length]
for root in (ShapeDoc, ShapeMsg):
    for form, write in writers(root).items():
        good = root()
        good.shapes = [shape(1, 5), shape(0), shape(2)]
        good.pair = [shape(0), shape(1, 65535)]
        write(good)
        for fault in faults:
            v = root()
            fault(v)
            try:
                write(v)
            except InvalidValue:
                continue
            raise AssertionError(f"{root.__name__} {fault.__name__} ({form}) was accepted")
print("ok")
`

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonPacketUnionArraysWriteRefusals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters (it runs the emitted table runtime)")
	u := loadUnitPaths(t, filepath.Join("..", "..", "..", "test", "tables", "packetunions", "PacketUnions.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, files map[string][]byte) []string {
		t.Helper()
		dir := t.TempDir()
		writeTree(t, dir, files)
		var outs []string
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + packetUnionWriteProbe)})
			if err != nil || strings.TrimSpace(stdout) != "ok" {
				outs = append(outs, filepath.Base(python)+": "+strings.TrimSpace(stdout+"\n"+stderr))
			}
		}
		return outs
	}
	for _, f := range run(t, files) {
		t.Error(f)
	}
	// NEGATIVE CONTROL: the table writer's union-array count check is planted
	// out of a COPY of the runtime, and the variable writer must then accept
	// the array counting past its bound, by name.
	t.Run("count-check-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "tblpacketunions/_schema/kind_union.py"
		const anchor = "        _check_union_count(f, len(value), path)\n"
		body := string(planted[path])
		if strings.Count(body, anchor) != 1 {
			t.Fatalf("%s carries the control anchor %d times, want 1", path, strings.Count(body, anchor))
		}
		planted[path] = []byte(strings.Replace(body, anchor, "", 1))
		outs := run(t, planted)
		if len(outs) != 2 {
			t.Fatalf("NEGATIVE CONTROL FAILED: %d of 2 pins went red: %q", len(outs), outs)
		}
		for _, out := range outs {
			if !strings.Contains(out, "ShapeDoc count_past_bound (variable) was accepted") {
				t.Errorf("NEGATIVE CONTROL FAILED: the planted fault is not the finding: %s", out)
			}
		}
	})
}
