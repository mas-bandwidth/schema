package ctable

// THE ROADMAP'S COMPILED-PLANS TASKS ON THE C LEG (docs/roadmap.sexp node
// `fixed-tables`, work-set `compiled-plans`; docs/FIXED-FORM-ALGORITHM.md §5.2's
// COMPILE(lock, T) and its THE STATIC DATA HAS MEMBER NAMES; §5.3's load and
// §5.9 #7's report; docs/SPEC-TABLES.md §3.4; ROADMAP.md "NEW Fixed Tables").
// One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus (the same stance as the t03 card).
//
//	c/R1  [verify]: COMPILE lays the lineage down as static data at build time,
//	                oldest first and the current layout last, from the lock.
//	c/R2  [weak]:   record_bytes is 8 + body: the lock stores the body, COMPILE
//	                adds the eight once, and no backend adds anything.
//	c/R23 [verify]: TableFixedKnownLayout = hash, layout, layout_bytes,
//	                record_bytes; the report's layout_hash last and zero on every
//	                other path.
//	c/R25 [owed]:   plan_too_large when the plan does not fit the caller's
//	                capacity.
//	c/R26 [owed]:   a known hash whose lineage entry would not build →
//	                layout_malformed / plan_too_large by that entry's own lane,
//	                never a throw.
//	c/W14 [owed]:   plan dst == offsetof/sizeof.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02NewerSrc and t02OlderSrc are the pair §5.1 permits: an appended field is
// the widening, so the older body is the newer's minus its last field and the
// two carry different layouts under different hashes.
const t02NewerSrc = `package probe

fixed table Row
{
    a int32 = 0
    b int32 = 0
}
`

const t02OlderSrc = `package probe

fixed table Row
{
    a int32 = 0
}
`

// t02Header is the one <Base>Table.h this leg emits for a unit WITH the older
// entry handed in as its lineage — the second entry point of §5.9 #1.
func t02Header(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatalf("no Table.h was emitted")
	return ""
}

// t02Fixture is the older/newer pair and the older entry the build is handed.
func t02Fixture(t *testing.T) (oU, nU *ir.Unit, older FixedLineageEntry) {
	t.Helper()
	oU = cUnitOf(t, t02OlderSrc)
	nU = cUnitOf(t, t02NewerSrc)
	e, ok := FixedLineageOf(oU, "Row")
	if !ok {
		t.Fatal("no lineage entry for Row")
	}
	return oU, nU, e
}

// t02NewerWithOlder is the newer table's header with the older layout handed in.
func t02NewerWithOlder(t *testing.T) (string, FixedLineageEntry, *ir.Struct) {
	t.Helper()
	_, nU, older := t02Fixture(t)
	text := t02Header(t, nU, map[string][]FixedLineageEntry{"Row": {older}})
	return text, older, t03Table(t, nU, "Row")
}

// t02Known is one row of the emitted `_fixed_known[]` array.
type t02Known struct {
	hash   uint64
	layout string
	record int64
}

var t02KnownRow = regexp.MustCompile(`\{\s*0x([0-9a-fA-F]{16})ull,\s*(\w+),\s*\(int64_t\)\s*sizeof\(\s*\w+\s*\),\s*(-?[0-9]+)\s*\}`)

// t02KnownRows parses the emitted known-layout rows IN SOURCE ORDER, so a
// lineage laid down out of order is read out of order.
func t02KnownRows(t *testing.T, text, snake string) []t02Known {
	t.Helper()
	marker := snake + "_fixed_known[] = {"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s_fixed_known", snake)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "\n};")
	if end < 0 {
		t.Fatalf("the emitted %s_fixed_known does not close", snake)
	}
	var out []t02Known
	for _, m := range t02KnownRow.FindAllStringSubmatch(rest[:end], -1) {
		h, err := strconv.ParseUint(m[1], 16, 64)
		if err != nil {
			t.Fatalf("known hash %q: %v", m[1], err)
		}
		rec, err := strconv.ParseInt(m[3], 10, 64)
		if err != nil {
			t.Fatalf("known record %q: %v", m[3], err)
		}
		out = append(out, t02Known{hash: h, layout: m[2], record: rec})
	}
	if len(out) == 0 {
		t.Fatalf("the emitted %s_fixed_known carries no rows", snake)
	}
	return out
}

// t02KnownCount reads `<snake>_fixed_known_count = <n>`.
func t02KnownCount(t *testing.T, text, snake string) int {
	t.Helper()
	marker := snake + "_fixed_known_count = "
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s_fixed_known_count", snake)
	}
	rest := text[at+len(marker):]
	end := strings.IndexAny(rest, ";\n")
	n, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		t.Fatalf("known count: %v", err)
	}
	return n
}

// t02Array parses a named `uint8_t <name>[] = { 0x.. , ... };` byte array.
func t02Array(t *testing.T, text, name string) []byte {
	t.Helper()
	marker := name + "[] = {"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s", name)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "};")
	if end < 0 {
		t.Fatalf("the emitted %s does not close", name)
	}
	var out []byte
	for _, f := range strings.Split(rest[:end], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("%s byte %q: %v", name, f, err)
		}
		out = append(out, byte(n))
	}
	return out
}

func TestFixedRoadmapCT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/R1", t02R1},
		{"c/R2", t02R2},
		{"c/R23", t02R23},
		{"c/R25", t02R25},
		{"c/R26", t02R26},
		{"c/W14", t02W14},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02R1: R1 "COMPILE lays the lineage down as static data at build time, oldest
// first and the current layout last, from the lock". §5.2's COMPILE: "for each
// entry x in lock.lineage(T), OLDEST FIRST — the current layout is the last of
// them ... emit R as static data: the hashes, the layout bytes, the record
// sizes, the plans, the floor". §5.2's lock table: "a LINEAGE, one entry per
// locked layout, OLDEST FIRST, the current one LAST".
func t02R1(t *testing.T) {
	text, older, st := t02NewerWithOlder(t)
	snake := ir.RustSnake(st.Name)
	own := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)

	// STATIC DATA AT BUILD TIME: the lineage is a const array, no file parsed
	// at load for it.
	if !strings.Contains(text, "static SCHEMA_UNUSED const TableFixedKnownLayout "+snake+"_fixed_known[]") {
		t.Errorf("R1: the lineage is not laid down as static data — no const %s_fixed_known[] (§5.2 COMPILE)", snake)
	}

	rows := t02KnownRows(t, text, snake)
	if len(rows) != 2 {
		t.Fatalf("R1: the emitted lineage carries %d entries, want 2 (the older layout and the current one)", len(rows))
	}
	if n := t02KnownCount(t, text, snake); n != len(rows) {
		t.Errorf("R1: %s_fixed_known_count = %d, want %d", snake, n, len(rows))
	}
	// OLDEST FIRST, THE CURRENT LAST.
	if rows[0].hash != older.Wire {
		t.Errorf("R1: the first lineage entry is 0x%016x, want the OLDER layout 0x%016x — oldest first", rows[0].hash, older.Wire)
	}
	if rows[len(rows)-1].hash != own {
		t.Errorf("R1: the last lineage entry is 0x%016x, want the CURRENT layout 0x%016x — the current one last", rows[len(rows)-1].hash, own)
	}
	// FROM THE LOCK: the older layout bytes are the handed entry's VERBATIM.
	got := t02Array(t, text, snake+"_fixed_known_0")
	if string(got) != string(older.Layout) {
		t.Errorf("R1: the emitted older layout is not the handed entry's bytes (the lock's own, verbatim) — %d bytes against %d", len(got), len(older.Layout))
	}
}

// t02R2: R2 "record_bytes is 8 + body: the lock stores the body, COMPILE adds
// the eight once, and no backend adds anything". §5.2: "per entry: the record
// BODY size | R.known[i].record_bytes as `8 + body`: the lock stores the BODY,
// COMPILE adds the eight hash bytes ONCE, and a BACKEND ADDS NOTHING. Never read
// off the file". The C leg's older entry is handed as an ALREADY-WHOLE record
// (the driver added the eight, compiler/lineage.go), so the emitter must write
// it through unchanged — a second addition is the "eight bytes long" defect.
func t02R2(t *testing.T) {
	text, older, st := t02NewerWithOlder(t)
	snake := ir.RustSnake(st.Name)
	rows := t02KnownRows(t, text, snake)
	if len(rows) != 2 {
		t.Fatalf("R2: want two lineage entries, got %d", len(rows))
	}
	// THE HANDED ENTRY IS ALREADY THE WHOLE RECORD (8 + body): the emitter adds
	// nothing.
	if rows[0].record != older.Record {
		t.Errorf("R2: the older entry is emitted with record_bytes %d, want the handed whole record %d — the emitter adds nothing (the eight were added once, upstream)", rows[0].record, older.Record)
	}
	if rows[0].record == older.Record+8 {
		t.Errorf("R2: the eight hash bytes were added a SECOND time (compiler/lineage.go already added them once)")
	}
	// THE CURRENT ENTRY: 8 + the body this leg computes.
	wantOwn := 8 + ir.TableFixedTypeBytes(st)
	if rows[1].record != wantOwn {
		t.Errorf("R2: the current entry is emitted with record_bytes %d, want 8 + body = %d", rows[1].record, wantOwn)
	}
	if !strings.Contains(text, "static SCHEMA_UNUSED const int64_t "+snake+"_fixed_record_bytes = 8 + ") {
		t.Errorf("R2: the current record size is not spelled 8 + body")
	}
	// THE READ TAKES THE RECORD SIZE FROM THE LOCK'S ENTRY, never recomputed
	// from the file (§5.2: "Never read off the file").
	if !strings.Contains(text, "record_bytes = "+snake+"_fixed_known[pick].record_bytes;") {
		t.Errorf("R2: the read does not take record_bytes from the selected lock entry — it must never read it off the file")
	}
}

// t02R23: R23 "the static data's member names and order — TableFixedKnownLayout
// = hash, layout, layout_bytes, record_bytes; the report's layout_hash last and
// zero on every other path". §5.2: "One entry of R.known is
// TableFixedKnownLayout and it carries FOUR members in this order — hash,
// layout, layout_bytes, record_bytes ... The file's hash lands on the report as
// layout_hash, last on the report and zero on every other path (§5.9 #15)".
func t02R23(t *testing.T) {
	// THE STATIC DATA'S MEMBER NAMES AND ORDER, out of the shared runtime body
	// the leg emits — the same text tools/fixedtwin holds to the reference.
	const open = "typedef struct TableFixedKnownLayout"
	at := strings.Index(tableFixedRuntime, open)
	if at < 0 {
		t.Fatalf("R23: the runtime declares no TableFixedKnownLayout")
	}
	body := tableFixedRuntime[at:]
	if end := strings.Index(body, "} TableFixedKnownLayout;"); end >= 0 {
		body = body[:end]
	} else {
		t.Fatalf("R23: the TableFixedKnownLayout declaration does not close")
	}
	order := []string{"hash", "layout", "layout_bytes", "record_bytes"}
	last := -1
	for _, m := range order {
		i := strings.Index(body, m)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout carries no member %q", m)
		}
		if i < last {
			t.Errorf("R23: the member %q is out of order — TableFixedKnownLayout is hash, layout, layout_bytes, record_bytes", m)
		}
		last = i
	}

	// THE REPORT'S layout_hash LAST, out of the emitted header.
	text, _, _ := t02NewerWithOlder(t)
	rat := strings.Index(text, "typedef struct TableReport")
	if rat < 0 {
		t.Fatalf("R23: the emitted header declares no TableReport")
	}
	rbody := text[rat:]
	if end := strings.Index(rbody, "} TableReport;"); end >= 0 {
		rbody = rbody[:end]
	} else {
		t.Fatalf("R23: the TableReport declaration does not close")
	}
	if !strings.Contains(rbody, "uint64_t layout_hash;") {
		t.Errorf("R23: the report carries no layout_hash member")
	}
	// it is LAST: no other member after it
	tail := rbody[strings.Index(rbody, "uint64_t layout_hash;")+len("uint64_t layout_hash;"):]
	if strings.Contains(tail, ";") {
		t.Errorf("R23: layout_hash is not the LAST member of the report")
	}

	// ZERO ON EVERY OTHER PATH: layout_hash is assigned in exactly one place in
	// the runtime, and it is the refusal that reports the file's hash.
	if n := strings.Count(tableFixedRuntime, "layout_hash = "); n != 1 {
		t.Errorf("R23: layout_hash is written %d times in the runtime, want exactly one (the backward-read refusal)", n)
	}
	refuse := tableFixedRuntime[strings.Index(tableFixedRuntime, "table_fixed_refuse_hash"):]
	if !strings.Contains(refuse, "report->layout_hash = hash;") {
		t.Errorf("R23: the one layout_hash store is not the file-hash refusal — zero on every other path (§5.9 #7)")
	}
}

// t02R25: R25 "plan_too_large when the plan does not fit the caller's capacity".
// §4.2: "The plan's storage is the CALLER's, declared by capacity; the codec
// never allocates, and a plan that does not fit is `REFUSE plan_too_large`".
// §5.2's COMPILE and §5.9 #4: an older entry whose plan is longer than the
// declared capacity refuses plan_too_large BY NAME.
func t02R25(t *testing.T) {
	text, _, st := t02NewerWithOlder(t)
	snake := ir.RustSnake(st.Name)
	if !strings.Contains(text, snake+"_fixed_lineage[pick].count > plan_capacity") {
		t.Errorf("R25: the load does not compare the selected plan's length against the caller's capacity")
	}
	if !strings.Contains(text, "report->reason = SCHEMA_TABLE_PLAN_TOO_LARGE;") {
		t.Errorf("R25: the over-capacity lane does not refuse plan_too_large BY NAME")
	}
	// AND IT IS A REFUSAL, NOT A MALFORMED READ: refused set, no malformed.
	if !strings.Contains(text, "SCHEMA_TABLE_PLAN_TOO_LARGE") {
		t.Errorf("R25: the emitted unit names no plan_too_large")
	}
	if !strings.Contains(tableFixedRuntime, "SCHEMA_TABLE_PLAN_TOO_LARGE = 9") {
		t.Errorf("R25: the runtime carries no plan_too_large reason")
	}
}

// t02R26: R26 "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large / layout_record_too_large by that entry's
// own lane, never a throw". §5.2's PLAN: an entry that will not parse is
// refused "at GENERATE ... the emitted entry is a LANE carrying that name,
// never a throw a first load discovers (§5.9 #36)". §5.9 #4: "an entry whose
// plan exceeds [the declared capacity] records plan_too_large ON THAT ENTRY";
// §5.9 #8: a lock bug carries layout_malformed "by its name"; a hostile layout
// (a third nested union the two-lane compiled walk refuses, past the depth
// bound) carries layout_record_too_large, the reference's name (§5.8 row 6).
func t02R26(t *testing.T) {
	text, _, st := t02NewerWithOlder(t)
	snake := ir.RustSnake(st.Name)
	// THE TWO LANES, EACH BY ITS OWN NAME, ON THE ENTRY.
	if !strings.Contains(text, snake+"_fixed_lineage[i].reason = SCHEMA_TABLE_LAYOUT_MALFORMED; continue;") {
		t.Errorf("R26: a lineage entry that does not parse carries no layout_malformed lane of its own")
	}
	if !strings.Contains(text, "( made == -2 ) ? SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE : SCHEMA_TABLE_PLAN_TOO_LARGE") {
		t.Errorf("R26: a lineage entry that will not build does not pick its own lane (layout_record_too_large vs plan_too_large)")
	}
	// AND LOAD REFUSES BY THAT ENTRY'S OWN LANE.
	if !strings.Contains(text, snake+"_fixed_lineage[pick].reason != 0") {
		t.Errorf("R26: the load does not refuse a file by the selected entry's own recorded reason")
	}
	// NEVER A THROW: no C `throw` statement anywhere in the emitted unit.
	if regexp.MustCompile(`\bthrow\b`).MatchString(text) {
		t.Errorf("R26: the emitted unit throws — a lineage entry that will not build is a LANE, never a throw")
	}
}

// t02W14: W14 "plan dst == offsetof/sizeof". §4.1's Op `dst` is "a byte offset
// into the READER's storage"; fixedform.go's opening note: "every offset and
// every size the plan depends on is asserted against offsetof and sizeof, so a
// layout this generator got wrong is a build error and never a wrong read."
func t02W14(t *testing.T) {
	text, _, st := t02NewerWithOlder(t)
	snake := ir.RustSnake(st.Name)
	// THE STATIC ASSERTS THE PLAN RESTS ON.
	offRe := regexp.MustCompile(`SCHEMA_TABLE_STATIC_ASSERT\( fixed_\w+_\w+, offsetof\( \w+, \w+ \) == (-?\d+)`)
	offVals := map[int64]bool{}
	for _, m := range offRe.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("W14: offsetof assert value %q: %v", m[1], err)
		}
		offVals[v] = true
	}
	if len(offVals) == 0 {
		t.Fatalf("W14: the emitted unit carries no offsetof static assert — the plan dst is not tied to the ABI")
	}
	if !regexp.MustCompile(`SCHEMA_TABLE_STATIC_ASSERT\( fixed_\w+_size, sizeof\( \w+ \) == \d+`).MatchString(text) {
		t.Errorf("W14: the emitted unit carries no sizeof static assert for the plan's type")
	}
	// EVERY PLAN dst IS ONE OF THOSE offsetof VALUES.
	planRe := regexp.MustCompile(`\{\s*\d+u,\s*(\d+)u,\s*`)
	at := strings.Index(text, snake+"_fixed_plan[] = {")
	if at < 0 {
		t.Fatalf("W14: the emitted unit carries no %s_fixed_plan", snake)
	}
	body := text[at:]
	end := strings.Index(body, "\n};")
	if end < 0 {
		t.Fatalf("W14: %s_fixed_plan does not close", snake)
	}
	dsts := planRe.FindAllStringSubmatch(body[:end], -1)
	if len(dsts) == 0 {
		t.Fatalf("W14: %s_fixed_plan carries no entries", snake)
	}
	for _, m := range dsts {
		v, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("W14: plan dst %q: %v", m[1], err)
		}
		if !offVals[v] {
			t.Errorf("W14: a plan dst is %d, which is no offsetof the static asserts vouch for — plan dst == offsetof (§4.1)", v)
		}
	}
}
