package goosetable_test

// RETAIN-UNKNOWN for Goose (docs/SPEC-TABLES.md §6.6), the gates the Go leg's
// cell names (internal/codegen/gotable/retain_test.go), held to the reference
// engine's retaining read and write (internal/tablewire DecodeRetain and
// EncodeRetain) rather than to a pin of Goose's own:
//
//	TestGooseRetainMatchesIndependentRewrite  a newer writer's unknown fields
//	    at every site a body can sit (a pointer, a union arm, a by-value
//	    child, an optional, a fixed and a counted array, a list, a union
//	    array, an enum-keyed array, a map, an unknown union arm): the
//	    retaining load's counters, and the retaining save's bytes, are the
//	    reference's, twice over, and a zero-capacity store keeps nothing and
//	    counts every unknown lost
//	TestGooseRetainReplacementAndShapeChanges  a retained body REPLACED in the
//	    same read (a repeated child, a cleared array, a keyed slot written
//	    twice, a switched union arm, a duplicate map key) keeps only the
//	    survivor's retained fields; a caller's shape edit that drops a body
//	    loses its retained record, by count
//
// The message form carries no retention for any table: it is fixed tables
// only (§3.3), and the conformance corpus has no retain-message row (ruling
// message-form-fixed-only, docs/rulings.json); compiler/messageformlock_test.go
// holds the Goose output to no variable-table message path.
//
// TestGooseRetainNegativeControl drops the retained tail from the save and
// requires both gates red.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const retainOldSchema = `package probe
enum Key { A, B }
fixed table Child { n int32 }
union Inner { child Child }
union Choice { child Child
 nested [..2]Inner
 empty }
table Root { link *Child
 direct Inner
 child Child
 optional ?Child
 array [..3]Child
 list []Child
 choices [..2]Choice
 keyed [Key]Child
 mapping map[int32]Child
 choice Choice }
`

const retainShapeSchema = `package probe
enum Key { A, B }
fixed table Child { n int32 }
union Choice { branch Child
 empty }
table Root { child Child
 array [..2]Child
 keyed [Key]Child
 choice Choice
 entries map[int32]Child
 link *Child }
`

// retainCase is one wire through the Goose retaining load and save.
type retainCase struct {
	name     string
	wire     []byte
	capacity int64 // the store's bytes, -1 for no ceiling
	edit     int64 // a counted array field whose live length the caller sets, -1 for none
	editLen  int64
}

// retainResult is what the probe printed for one case.
type retainResult struct {
	refused                         bool
	unknown, retained, lost, stored int64 // the load's counters and the live records
	saveRefusal                     string
	saveLost                        int64 // retain_lost the save added
	saved, again                    []byte
	reloadUnknown                   int64 // unknown counted by a plain load of the saved bytes
}

const retainProbeMain = `
fn retain_case(name: const u8[:], cap: i64, edit: i64, edit_len: i64) {
    let data = input(str(name, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: cap, id_capacity: -1, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let root = schema_rt::load_table_retain(conformance_unit(), "Root", data, vals, nodes, report, store);
    if root < 0 { write_stdout(str(name, " refused\n")); return; }
    var stored: i64 = 0;
    for r in store.recs { if !r.dead { stored += 1; } }
    let unknown = report.unknown;
    let retained = report.retained;
    let lost = report.retain_lost;
    if edit >= 0 { vals[vals[root].first + edit].len = edit_len; }
    var out: u8[>..<] = [];
    let refusal = schema_rt::save_table_retain(conformance_unit(), "Root", data, vals, nodes, root, store, report, out);
    var again: u8[>..<] = [];
    var second = schema_rt::LoadReport { .. };
    let refusal_again = schema_rt::save_table_retain(conformance_unit(), "Root", data, vals, nodes, root, store, second, again);
    if refusal_again != refusal { write_stderr(str(name, ": the second save answered another refusal\n")); }
    var rv: schema_rt::LoadVal[>..] = [];
    var rn: schema_rt::LoadNode[>..] = [];
    var rr = schema_rt::LoadReport { .. };
    var reload: i64 = -1;
    var saved: u8[>..] = [];
    for x in out { saved.push(x); }
    if schema_rt::load_table(conformance_unit(), "Root", saved, rv, rn, rr) >= 0 { reload = rr.unknown; }
    write_stdout(str(name, " ", unknown, " ", retained, " ", lost, " ", stored, " ", schema_rt::save_refusal_name(refusal), "_ ",
        report.retain_lost - lost, " ", hex(out), " ", hex(again), " ", reload, "\n"));
}

fn main() {
`

// gooseRetainProbe runs every case through one generated program.
func gooseRetainProbe(t *testing.T, u *ir.Unit, cases []retainCase, override map[string][]byte) map[string]retainResult {
	t.Helper()
	files := probeFiles(t, u, override)
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(retainProbeMain)
	for _, c := range cases {
		files[c.name+".bin"] = c.wire
		fmt.Fprintf(&main, "    retain_case(%q, %d, %d, %d);\n", c.name, c.capacity, c.edit, c.editLen)
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	out := gooserun.Run(t, t.TempDir(), "main.goose", files)
	got := map[string]retainResult{}
	for name, f := range probeLines(out) {
		if len(f) == 1 && f[0] == "refused" {
			got[name] = retainResult{refused: true}
			continue
		}
		if len(f) != 9 {
			t.Fatalf("%s: the probe printed %q", name, f)
		}
		n := make([]int64, 9)
		for _, i := range []int{0, 1, 2, 3, 5, 8} {
			v, err := strconv.ParseInt(f[i], 10, 64)
			if err != nil {
				t.Fatalf("%s: %q is not a count", name, f[i])
			}
			n[i] = v
		}
		got[name] = retainResult{
			unknown: n[0], retained: n[1], lost: n[2], stored: n[3],
			saveRefusal: strings.TrimSuffix(f[4], "_"), saveLost: n[5],
			saved: probeHex(t, f[6]), again: probeHex(t, f[7]), reloadUnknown: n[8],
		}
	}
	return got
}

// oracleRetain is the reference engine's retaining read of wire as reader's
// root and its retaining write of what it kept.
func oracleRetain(t *testing.T, reader *ir.Unit, wire []byte, capacity int) (tabletext.Report, []byte) {
	t.Helper()
	m := tabletext.NewModel(reader)
	v := m.New(m.Lookup("Root"))
	store := &tablewire.Retain{Capacity: capacity, IdCapacity: 1024}
	var report tabletext.Report
	ok, err := tablewire.DecodeRetain(m, v, wire, store, &report)
	if !ok || err != nil || report.Refused {
		t.Fatalf("the reference refuses the wire: %v %v %+v", ok, err, report)
	}
	want, err := tablewire.EncodeRetain(m, v, store, &report)
	if err != nil {
		t.Fatal(err)
	}
	return report, want
}

// independentRewrite is the rewrite gate's red list, empty when green.
func independentRewrite(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	newer := strings.Replace(retainOldSchema, "table Child { n int32 }", "table Child { n int32\n extra string(16) }", 1)
	newer = strings.Replace(newer, " choice Choice }", " choice Choice\n future Choice }", 1)
	writer := probeUnit(t, newer)
	model := tabletext.NewModel(writer)
	v := model.New(model.Lookup("Root"))
	var report tabletext.Report
	text := `{"link":{"extra":"node"},"child":{"extra":"child"},"optional":{"extra":"optional"},"array":[{"extra":"array0"},{"n":3,"extra":"array1"}],"list":[{"extra":"list"}],"choices":[{"child":{"extra":"arm0"}},{"nested":[{"child":{"extra":"nested0"}},{"child":{"extra":"nested1"}}]}],"keyed":{"A":{"extra":"keyA"},"B":{"extra":"keyB"}},"mapping":{"-2":{"extra":"map"}},"choice":{"child":{"extra":"arm"}},"future":{"child":{"extra":"unknown union"}}}`
	if !model.Read(v, []byte(text), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(model, v)
	if err != nil {
		t.Fatal(err)
	}
	reader := probeUnit(t, retainOldSchema)
	ref, want := oracleRetain(t, reader, wire, 1<<20)
	if ref.Retained == 0 {
		t.Fatal("the reference retained nothing, so the gate would watch nothing")
	}
	got := gooseRetainProbe(t, reader, []retainCase{
		{"full", wire, -1, -1, 0},
		{"zero", wire, 0, -1, 0},
	}, override)
	var bad []string
	full, zero := got["full"], got["zero"]
	if full.refused || zero.refused {
		return []string{"the Goose retaining load refused the newer writer's wire"}
	}
	if full.unknown != int64(ref.Unknown) || full.retained != int64(ref.Retained) || full.lost != int64(ref.RetainLost) {
		bad = append(bad, fmt.Sprintf("load counters unknown/retained/lost %d/%d/%d, the reference %d/%d/%d",
			full.unknown, full.retained, full.lost, ref.Unknown, ref.Retained, ref.RetainLost))
	}
	if full.lost+full.saveLost != int64(ref.RetainLost) {
		bad = append(bad, fmt.Sprintf("load and save lost %d+%d, the reference %d", full.lost, full.saveLost, ref.RetainLost))
	}
	if full.saveRefusal != "" || full.saveLost != 0 {
		bad = append(bad, fmt.Sprintf("the retaining save refused %q or lost %d", full.saveRefusal, full.saveLost))
	}
	if !bytes.Equal(full.saved, want) {
		bad = append(bad, fmt.Sprintf("the retaining save wrote %d bytes, the reference's rewrite is %d (first difference at %d)", len(full.saved), len(want), firstDifference(full.saved, want)))
	}
	if !bytes.Equal(full.again, full.saved) {
		bad = append(bad, "a second retaining save of the same value wrote other bytes")
	}
	if zero.retained != 0 || zero.lost != int64(ref.Retained+ref.RetainLost) {
		bad = append(bad, fmt.Sprintf("a zero-capacity store retained %d and lost %d, want 0 and %d", zero.retained, zero.lost, ref.Retained+ref.RetainLost))
	}
	t.Logf("the reference: unknown %d, retained %d, retain_lost %d, rewrite %d bytes", ref.Unknown, ref.Retained, ref.RetainLost, len(want))
	return bad
}

func firstDifference(a, b []byte) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

func TestGooseRetainMatchesIndependentRewrite(t *testing.T) {
	t.Parallel()
	for _, b := range independentRewrite(t, nil) {
		t.Error(b)
	}
}

// shapeWire is the Go leg's hand-built file: the form byte, the root body,
// the id table of the twelve names, and its count.
func shapeWire(payload []byte) []byte {
	names := []string{"child", "array", "keyed", "choice", "entries", "extra", "branch", "empty", "A", "B", "key", "value"}
	wire := append(append([]byte{1}, payload...), 0)
	for _, name := range names {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(names)))
}

// shapeCases are the Go leg's cases (TestRetainReplacementAndShapeChanges),
// with the counters it pins: retained, the live records stored, and the
// retain_lost the save adds.
func shapeCases() []struct {
	retainCase
	retained, stored, lost int64
} {
	field := func(ref, kind byte, payload ...byte) []byte { return append([]byte{ref, kind}, payload...) }
	join := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	body := func(p []byte) []byte { return append([]byte{byte(len(p))}, p...) }
	unknown, empty := []byte{6, 6, 99, 0}, []byte{0}
	child := field(1, 13, body(unknown)...)
	two := field(2, 14, body(join([]byte{13, 2}, body(unknown), body(unknown)))...)
	keyed := func(key byte, p []byte) []byte { return field(3, 16, body(join([]byte{13, 1, key}, body(p)))...) }
	arm := field(4, 15, join([]byte{7, 13}, body(unknown))...)
	entry := func(p []byte) []byte { return join([]byte{11, 4, 7, 0, 0, 0}, field(12, 13, body(p)...), []byte{0}) }
	type sc = struct {
		retainCase
		retained, stored, lost int64
	}
	return []sc{
		{retainCase{"repeated_child", shapeWire(join(child, field(1, 13, 1, 0))), -1, -1, 0}, 1, 0, 0},
		{retainCase{"cleared_array", shapeWire(join(two, field(2, 14, 2, 13, 0))), -1, -1, 0}, 2, 0, 0},
		{retainCase{"inert_array", shapeWire(join(two, field(2, 14, 0))), -1, -1, 0}, 2, 2, 0},
		{retainCase{"keyed_slots", shapeWire(join(keyed(9, unknown), keyed(10, unknown), keyed(9, empty))), -1, -1, 0}, 2, 1, 0},
		{retainCase{"switched_union", shapeWire(join(arm, field(4, 15, 8, 32, 0))), -1, -1, 0}, 1, 0, 0},
		{retainCase{"duplicate_map_key", shapeWire(field(5, 14, body(join([]byte{13, 2}, body(entry(unknown)), body(entry(empty))))...)), -1, -1, 0}, 1, 0, 0},
		// the caller drops the second element: its retained record is lost
		{retainCase{"shape_edit", shapeWire(two), -1, 1, 1}, 2, 2, 1},
	}
}

// replacementAndShape is the replacement gate's red list, empty when green.
func replacementAndShape(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	u := probeUnit(t, retainShapeSchema)
	cases := shapeCases()
	var rc []retainCase
	for _, c := range cases {
		rc = append(rc, c.retainCase)
	}
	got := gooseRetainProbe(t, u, rc, override)
	var bad []string
	for _, c := range cases {
		g := got[c.name]
		if g.refused {
			bad = append(bad, c.name+": the retaining load refused")
			continue
		}
		if g.retained != c.retained || g.stored != c.stored || g.lost != 0 {
			bad = append(bad, fmt.Sprintf("%s: load retained %d, stored %d, lost %d; want %d, %d, 0", c.name, g.retained, g.stored, g.lost, c.retained, c.stored))
		}
		if g.saveRefusal != "" || g.saveLost != c.lost {
			bad = append(bad, fmt.Sprintf("%s: save refused %q, lost %d; want none, %d", c.name, g.saveRefusal, g.saveLost, c.lost))
		}
		if g.reloadUnknown != c.stored-c.lost {
			bad = append(bad, fmt.Sprintf("%s: the saved bytes carry %d unknown fields, want %d", c.name, g.reloadUnknown, c.stored-c.lost))
		}
		if c.edit >= 0 {
			continue // the reference has no caller edit to replay
		}
		ref, want := oracleRetain(t, u, c.wire, 1<<20)
		if g.retained != int64(ref.Retained) || g.unknown != int64(ref.Unknown) {
			bad = append(bad, fmt.Sprintf("%s: load unknown/retained %d/%d, the reference %d/%d", c.name, g.unknown, g.retained, ref.Unknown, ref.Retained))
		}
		if g.lost+g.saveLost != int64(ref.RetainLost) {
			bad = append(bad, fmt.Sprintf("%s: load and save lost %d+%d, the reference %d", c.name, g.lost, g.saveLost, ref.RetainLost))
		}
		if !bytes.Equal(g.saved, want) {
			bad = append(bad, fmt.Sprintf("%s: the retaining save differs from the reference's rewrite at byte %d", c.name, firstDifference(g.saved, want)))
		}
	}
	return bad
}

func TestGooseRetainReplacementAndShapeChanges(t *testing.T) {
	t.Skip("layer 4 (goose): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	t.Parallel()
	for _, b := range replacementAndShape(t, nil) {
		t.Error(b)
	}
}

// TestGooseRetainNegativeControl:
//
//   - the save writes no retained tail (§6.6): both gates must go red;
//   - a shorter repeat of a counted array keeps the dropped elements'
//     retained records: the replacement gate's cleared_array row must go red,
//     and the rewrite gate stays green.
func TestGooseRetainNegativeControl(t *testing.T) {
	t.Skip("layer 4 (goose): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	t.Parallel()
	t.Run("no-tail", func(t *testing.T) {
		t.Parallel()
		o := sabotage(t, "wire_retain.goose",
			"    if rec < 0 { return; }\n    for i in c.retain.recs.len {",
			"    if rec < 0 || rec >= 0 { return; }\n    for i in c.retain.recs.len {")
		if bad := independentRewrite(t, o); len(bad) == 0 {
			t.Error("the rewrite gate stayed green with no retained tail written")
		} else {
			t.Logf("rewrite gate red: %s", bad[0])
		}
		if bad := replacementAndShape(t, o); len(bad) == 0 {
			t.Error("the replacement gate stayed green with no retained tail written")
		} else {
			t.Logf("replacement gate red: %d findings, first %s", len(bad), bad[0])
		}
	})
	t.Run("counted-repeat", func(t *testing.T) {
		t.Parallel()
		o := sabotage(t, "wire_load.goose", "                    if c.retaining { forget_rec(c, first + k); }\n", "")
		bad := replacementAndShape(t, o)
		if len(bad) == 0 {
			t.Fatal("the replacement gate stayed green with the dropped elements' records kept")
		}
		for _, b := range bad {
			if !strings.HasPrefix(b, "cleared_array:") {
				t.Errorf("a row the break does not own went red: %s", b)
			}
		}
		if rewrite := independentRewrite(t, o); len(rewrite) > 0 {
			t.Errorf("the rewrite gate went red too: %s", rewrite[0])
		}
		t.Logf("replacement gate red on cleared_array only: %s", bad[0])
	})
}
