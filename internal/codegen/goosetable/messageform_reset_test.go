package goosetable_test

// A REFUSED BATCH LEAVES EVERY BODY AT ITS DECLARED DEFAULTS
// (docs/SPEC-TABLES.md §3.3, §4; docs/rulings.json strict-only-read). The
// caller's whole batch is discarded on every refusal, whether the reason rides
// the report (a non-finite float, an out-of-range value) or the read is
// damaged: no body of the batch is a value to use, the bodies before the
// refusal included.

import (
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// resetCases is one refusal the body walk can raise and the answer the batch
// load must give: the return, the reason, whether the read is damaged, and
// that both bodies are back at their declared defaults.
type resetCase struct {
	name, n, reason, damaged string
}

var resetCases = []resetCase{
	{"non_finite_float", "-1", "non_finite_float", "0"},
	{"out_of_range", "-1", "out_of_range", "0"},
	{"damaged_framing", "0", "ok", "1"},
}

func TestGooseLoadMessagesRefusalResetsBatch(t *testing.T) {
	t.Parallel()
	u := resetUnit(t)
	lines := resetProbe(t, u, nil)
	if errs := checkResetBodies(lines); len(errs) > 0 {
		t.Fatalf("a refused batch left a body off its declared defaults:\n%s", strings.Join(errs, "\n"))
	}
}

// TestGooseLoadMessagesRefusalResetsBatchNegativeControl restores the
// reasoned-refusal exit's early return in a copy of the generated reader: the
// bodies the walk reached before the refusal keep their loaded values, and
// the check above goes red naming the case.
func TestGooseLoadMessagesRefusalResetsBatchNegativeControl(t *testing.T) {
	t.Parallel()
	u := resetUnit(t)
	baseline := resetProbe(t, u, nil)
	if errs := checkResetBodies(baseline); len(errs) > 0 {
		t.Fatalf("the control needs a clean baseline:\n%s", strings.Join(errs, "\n"))
	}
	changed := resetUnitFiles(t, u, nil)
	anchor := "            if r.reason != 0 {\n                report.reason = r.reason;\n                reasoned = true;\n                break;\n            }\n"
	early := "            if r.reason != 0 {\n                report.reason = r.reason;\n                return -1;\n            }\n"
	replaced := 0
	for name, data := range changed {
		if strings.Count(string(data), anchor) == 0 {
			continue
		}
		changed[name] = []byte(strings.ReplaceAll(string(data), anchor, early))
		replaced += strings.Count(string(data), anchor)
	}
	if replaced < len(resetCases)-1 {
		t.Fatalf("the reasoned-refusal reset anchor occurs %d times, want at least %d", replaced, len(resetCases)-1)
	}
	got := resetProbe(t, u, changed)
	errs := checkResetBodies(got)
	for _, name := range []string{"non_finite_float", "out_of_range"} {
		found := false
		for _, err := range errs {
			if strings.HasPrefix(err, name+":") {
				found = true
			}
		}
		if !found {
			t.Fatalf("removing the reasoned-refusal reset did not turn %s red:\n%s", name, strings.Join(errs, "\n"))
		}
	}
}

func checkResetBodies(lines map[string][]string) []string {
	var errs []string
	for _, c := range resetCases {
		got, ok := lines[c.name]
		if !ok {
			errs = append(errs, c.name+": the probe printed no line")
			continue
		}
		if len(got) != 6 {
			errs = append(errs, fmt.Sprintf("%s: the probe printed %d fields, want 6: %s", c.name, len(got), strings.Join(got, " ")))
			continue
		}
		if got[1] != c.n || got[2] != c.reason || got[3] != c.damaged {
			errs = append(errs, fmt.Sprintf("%s: LoadMessages answered n=%s reason=%s damaged=%s, want n=%s reason=%s damaged=%s", c.name, got[1], got[2], got[3], c.n, c.reason, c.damaged))
		}
		if got[4] != "1" || got[5] != "1" {
			errs = append(errs, fmt.Sprintf("%s: bodies at their declared defaults: body 0=%s body 1=%s, want 1 1", c.name, got[4], got[5]))
		}
	}
	return errs
}

func resetUnit(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	return u
}

func resetUnitFiles(t *testing.T, u *ir.Unit, override map[string][]byte) map[string][]byte {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, override)
	return files
}

const resetProbeBody = `
fn gunner_defaults(v: %[1]s::GunnerConfig) -> bool {
    let d = %[1]s::GunnerConfig { .. };
    return schema_rt::fixed_f32_bits(v.Reaction) == schema_rt::fixed_f32_bits(d.Reaction) && v.Tracking == d.Tracking;
}

fn team_defaults(v: %[1]s::TeamConfig) -> bool {
    let d = %[1]s::TeamConfig { .. };
    if v.SpawnCount != d.SpawnCount { return false; }
    if v.BannerLength != d.BannerLength { return false; }
    for i in 16 {
        if v.Banner[i] != d.Banner[i] { return false; }
    }
    return true;
}

fn gunner_case(name: const u8[:], file: const u8[:], voc: const schema_rt::MsgVocabulary&) {
    var data: u8[>..] = [];
    if !read_file(file, data) { print(name, " read-failed"); return; }
    var values: %[1]s::GunnerConfig[2] = [%[1]s::GunnerConfig { .. }; 2];
    var k: i64 = 0;
    while k < 2 {
        values[k].Reaction = 99.0;
        values[k].Tracking = true;
        k += 1;
    }
    var rep = schema_rt::MsgReport { .. };
    let n = %[1]s::GunnerConfigLoadMessages(values[0..2], voc, data, rep);
    print(name, " ", n, " ", schema_rt::msg_reason_name(rep.reason), " ", (if rep.damaged { 1 } else { 0 }), " ", (if gunner_defaults(values[0]) { 1 } else { 0 }), " ", (if gunner_defaults(values[1]) { 1 } else { 0 }));
}

fn team_case(name: const u8[:], file: const u8[:], voc: const schema_rt::MsgVocabulary&) {
    var data: u8[>..] = [];
    if !read_file(file, data) { print(name, " read-failed"); return; }
    var values: %[1]s::TeamConfig[2] = [%[1]s::TeamConfig { .. }; 2];
    var k: i64 = 0;
    while k < 2 {
        values[k].SpawnCount = 77;
        values[k].Banner[0] = 90;
        values[k].Banner[1] = 90;
        values[k].Banner[2] = 90;
        values[k].BannerLength = 3;
        k += 1;
    }
    var rep = schema_rt::MsgReport { .. };
    let n = %[1]s::TeamConfigLoadMessages(values[0..2], voc, data, rep);
    print(name, " ", n, " ", schema_rt::msg_reason_name(rep.reason), " ", (if rep.damaged { 1 } else { 0 }), " ", (if team_defaults(values[0]) { 1 } else { 0 }), " ", (if team_defaults(values[1]) { 1 } else { 0 }));
}

fn main() {
    var peer: u8[>..] = [];
    if !read_file("announce.bin", peer) { print("announce-failed"); return; }
    var voc = schema_rt::MsgVocabulary { .. };
    var vrep = schema_rt::MsgReport { .. };
    if !schema_rt::announce_read(voc, peer, vrep) { print("announce-refused"); return; }
    gunner_case("non_finite_float", "non_finite.bin", voc);
    team_case("out_of_range", "out_of_range.bin", voc);
    gunner_case("damaged_framing", "damaged.bin", voc);
}
`

func resetProbe(t *testing.T, u *ir.Unit, override map[string][]byte) map[string][]string {
	t.Helper()
	files := resetUnitFiles(t, u, override)
	ns := goose.Namespace(u)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	fmt.Fprintf(&b, resetProbeBody, ns)
	files["main.goose"] = []byte(b.String())
	files["announce.bin"] = tablewire.Announce(u)
	m := tabletext.NewModel(u)
	files["non_finite.bin"] = nonFiniteBatch(t, m)
	files["out_of_range.bin"] = outOfRangeBatch(t, m)
	files["damaged.bin"] = damagedBatch(t, m)
	out := strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "main.goose", files)))
	lines := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			lines[fields[0]] = fields
		}
	}
	return lines
}

func resetField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("%s has no field %s", inst.Def.Name, name)
	return nil
}

func encodeResetBatch(t *testing.T, m *tabletext.Model, root string, bodies ...func(*tabletext.Instance)) []byte {
	t.Helper()
	st := m.Lookup(root)
	insts := make([]*tabletext.Instance, len(bodies))
	for i, body := range bodies {
		inst := m.New(st)
		if body != nil {
			body(inst)
		}
		insts[i] = inst
	}
	batch, err := tablewire.EncodeMessages(m, insts)
	if err != nil {
		t.Fatalf("encode %s batch: %v", root, err)
	}
	return batch
}

func nonFiniteBatch(t *testing.T, m *tabletext.Model) []byte {
	t.Helper()
	carry := func(inst *tabletext.Instance) {
		resetField(t, inst, "reaction").Cell.F = 7.25
		resetField(t, inst, "tracking").Cell.B = true
	}
	set := func(bits uint32) func(*tabletext.Instance) {
		return func(inst *tabletext.Instance) {
			resetField(t, inst, "reaction").Cell.F = float64(math.Float32frombits(bits))
		}
	}
	a := encodeResetBatch(t, m, "GunnerConfig", carry, set(math.Float32bits(1.0)))
	b := encodeResetBatch(t, m, "GunnerConfig", carry, set(^math.Float32bits(1.0)))
	return patchBatchValue(t, a, b, 32, uint64(math.Float32bits(float32(math.NaN()))))
}

func outOfRangeBatch(t *testing.T, m *tabletext.Model) []byte {
	t.Helper()
	carry := func(inst *tabletext.Instance) {
		resetField(t, inst, "spawn_count").Cell.I = 7
	}
	set := func(v int64) func(*tabletext.Instance) {
		return func(inst *tabletext.Instance) {
			resetField(t, inst, "spawn_count").Cell.I = v
		}
	}
	a := encodeResetBatch(t, m, "TeamConfig", carry, set(63))
	b := encodeResetBatch(t, m, "TeamConfig", carry, set(64))
	return patchBatchValue(t, a, b, 7, 65)
}

func damagedBatch(t *testing.T, m *tabletext.Model) []byte {
	t.Helper()
	set := func(inst *tabletext.Instance) {
		resetField(t, inst, "reaction").Cell.F = 7.25
		resetField(t, inst, "tracking").Cell.B = true
	}
	batch := encodeResetBatch(t, m, "GunnerConfig", set, set)
	return batch[:len(batch)-2]
}

// patchBatchValue finds a value's bits as the positions two batches that
// differ only in that value disagree on, and writes v there instead.
func patchBatchValue(t *testing.T, valid, other []byte, width int, v uint64) []byte {
	t.Helper()
	if len(valid) != len(other) {
		t.Fatalf("the two batches are %d and %d bytes, want one length", len(valid), len(other))
	}
	var pos []int
	for i := range valid {
		x := valid[i] ^ other[i]
		for bit := 0; bit < 8; bit++ {
			if x&(1<<uint(bit)) != 0 {
				pos = append(pos, i*8+bit)
			}
		}
	}
	if len(pos) != width {
		t.Fatalf("the value's bits are %d positions, want %d", len(pos), width)
	}
	lo := pos[0]
	for i, p := range pos {
		if p != lo+i {
			t.Fatalf("the value's bit positions are not contiguous: %v", pos)
		}
	}
	out := append([]byte(nil), valid...)
	for i := 0; i < width; i++ {
		setBatchBit(out, lo+i, v>>uint(i)&1 == 1)
	}
	return out
}

func setBatchBit(b []byte, at int, one bool) {
	if one {
		b[at/8] |= 1 << uint(at%8)
	} else {
		b[at/8] &^= 1 << uint(at%8)
	}
}
