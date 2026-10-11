package ctable

// THE ROADMAP'S COMPILED-PLANS TASKS ON THE C LEG (docs/roadmap.sexp node
// `fixed-tables`, row `compiled-plans` "Static plans, record sizes and caller
// capacity"; docs/FIXED-FORM-ALGORITHM.md §5.2's COMPILE(lock, T), §5.3's load,
// §5.9 #3/#4/#5/#7; docs/SPEC-TABLES.md §3.4; ROADMAP.md "NEW Fixed Tables").
// One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE TEXT THIS LEG EMITS — the static lineage, the
// record size, the TableFixedKnownLayout and TableReport shapes, and the load's
// own lanes — so the harness is reachable on a tree that never built the C++
// reference corpus.
//
//	c/R1  [verify]: COMPILE lays the lineage down as static data at build time,
//	               oldest first and the current layout last, from the lock.
//	c/R2  [weak]:   record_bytes is 8 + body: the lock stores the body, COMPILE
//	               adds the eight once, and no backend adds anything.
//	c/R23 [verify]: the static data's member names and order —
//	               TableFixedKnownLayout = hash, layout, layout_bytes,
//	               record_bytes; the report's layout_hash last and zero on every
//	               other path.
//	c/R25 [owed]:   plan_too_large when the plan does not fit the caller's
//	               capacity. HELD by fixedroadmap_t01_test.go's
//	               TestFixedRoadmapT01Framing/plan_too_large (capacity 0 over an
//	               older entry's lane), so it carries no subtest here.
//	c/R26 [owed]:   a known hash whose lineage entry would not build →
//	               layout_malformed / plan_too_large by that entry's own lane,
//	               never a throw. Asserted here on the emitted load lane.
//	c/W14 [owed]:   plan dst == offsetof/sizeof.

import (
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Files emits a unit with an older generation handed in as its lineage, and
// returns every generated file as text. It is the in-process twin of
// cRunVersionProbe's generate half, so a text assertion runs with no corpus.
func t02Files(t *testing.T, reader string, older []string) map[string]string {
	t.Helper()
	u := cUnitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := cUnitOf(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate lineage: %v", err)
	}
	maps.Copy(files, tables)
	out := map[string]string{}
	for name, data := range files {
		out[name] = string(data)
	}
	return out
}

// t02Text flattens every generated file into one string.
func t02Text(files map[string]string) string {
	var b strings.Builder
	for _, data := range files {
		b.WriteString(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// t02KnownRows returns the data rows of `<snake>_fixed_known[]`, each row's
// text, in order of appearance.
func t02KnownRows(t *testing.T, text, snake string) []string {
	t.Helper()
	marker := snake + "_fixed_known[] = {"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s_fixed_known", snake)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "};")
	if end < 0 {
		t.Fatalf("the published %s_fixed_known does not close", snake)
	}
	var rows []string
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// t02HashConst reads a published `0x…ull` constant out of the generated source.
func t02HashConst(t *testing.T, text, name string) string {
	t.Helper()
	marker := name + " = 0x"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s", name)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "ull")
	if end < 0 {
		t.Fatalf("the published %s has no end", name)
	}
	return "0x" + strings.TrimSpace(rest[:end]) + "ull"
}

func TestFixedRoadmapCT02Plans(t *testing.T) {
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/R1", t02R1},
		{"c/R2", t02R2},
		{"c/R23", t02R23},
		{"c/R26", t02R26},
		{"c/W14", t02W14},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// t02R1: §5.2 COMPILE(lock, T): "the locked layouts, OLDEST FIRST, the current
// one LAST. … [fixedLineage] appends the table's OWN entry last when the
// lineage does not already end on it." The emitted `…_fixed_known[]` rows are
// the static data: the oldest entry's symbol first, the current layout's own
// symbol and hash last.
func t02R1(t *testing.T) {
	reader := cReadSchema(t, "VNEW_field_append")
	files := t02Files(t, reader, []string{cReadSchema(t, "VOLD_field_append")})
	text := t02Text(files)
	snake := ir.RustSnake(cFixedRootName(t, cUnitOf(t, reader)))

	rows := t02KnownRows(t, text, snake)
	if len(rows) != 2 {
		t.Fatalf("%s_fixed_known carries %d rows, want 2 (the one older entry and the current one last): %v", snake, len(rows), rows)
	}
	if !strings.Contains(rows[0], snake+"_fixed_known_0") {
		t.Fatalf("the OLDER entry is not the first row: %s", rows[0])
	}
	if !strings.Contains(rows[len(rows)-1], snake+"_fixed_layout") {
		t.Fatalf("the CURRENT layout is not the last row: %s", rows[len(rows)-1])
	}
	own := t02HashConst(t, text, snake+"_fixed_hash")
	if !strings.Contains(rows[len(rows)-1], own) {
		t.Fatalf("the last row does not carry the current hash %s: %s", own, rows[len(rows)-1])
	}
	if strings.Contains(rows[0], own) {
		t.Fatalf("the OLDER row carries the current hash %s: the current layout is not last", own)
	}
}

// t02R2: §5.2's table and §5.9 #4: "Record is the body size, taken from the
// lock and never from the file", and the header's published
// `…_fixed_record_bytes = 8 + <body>` — the eight hash bytes added ONCE by
// COMPILE, and every lineage row carrying that same 8 + body so no backend
// adds anything.
func t02R2(t *testing.T) {
	reader := cReadSchema(t, "VNEW_field_append")
	files := t02Files(t, reader, []string{cReadSchema(t, "VOLD_field_append")})
	text := t02Text(files)
	snake := ir.RustSnake(cFixedRootName(t, cUnitOf(t, reader)))

	body := t02Number(t, text, snake+"_fixed_body_bytes")
	marker := snake + "_fixed_record_bytes = 8 + "
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s_fixed_record_bytes = 8 + <body>", snake)
	}
	rest := text[at+len(marker):]
	end := strings.IndexByte(rest, ';')
	if end < 0 {
		t.Fatalf("the published %s_fixed_record_bytes has no end", snake)
	}
	got, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
	if err != nil {
		t.Fatalf("the published record body %q is not a number: %v", rest[:end], err)
	}
	if got != body {
		t.Fatalf("%s_record_bytes is 8 + %d but %s_body_bytes is %d", snake, got, snake, body)
	}

	rows := t02KnownRows(t, text, snake)
	if len(rows) != 2 {
		t.Fatalf("%s_fixed_known carries %d rows, want 2", snake, len(rows))
	}
	// THE OLDER ROW carries the record the lock stored for that generation:
	// 8 + the OLDER body, its eight added once and none of this build's.
	older := cFixedRootName(t, cUnitOf(t, cReadSchema(t, "VOLD_field_append")))
	olderRecord := 8 + ir.TableFixedTypeBytes(cUnitOf(t, cReadSchema(t, "VOLD_field_append")).Tables[older])
	if !strings.HasSuffix(rows[0], ", "+strconv.FormatInt(olderRecord, 10)+" },") {
		t.Fatalf("the older lineage row does not carry its own lock record %d: %s", olderRecord, rows[0])
	}
	if !strings.HasSuffix(rows[1], ", "+strconv.FormatInt(8+body, 10)+" },") {
		t.Fatalf("the current lineage row does not carry record_bytes 8 + %d: %s", body, rows[1])
	}
}

// t02R23: the runtime's static-data member names and order (§5.2, §5.9 #7).
// TableFixedKnownLayout is exactly hash, layout, layout_bytes, record_bytes;
// TableReport's layout_hash is its LAST member; and the only assignment to
// layout_hash in the whole emitted unit is the one in table_fixed_refuse_hash,
// so it is zero on every other path.
func t02R23(t *testing.T) {
	files := t02Files(t, cReadSchema(t, "VNEW_field_append"), []string{cReadSchema(t, "VOLD_field_append")})
	text := t02Text(files)

	known := t02Struct(t, text, "TableFixedKnownLayout")
	want := []string{"uint64_t hash;", "const uint8_t * layout;", "int64_t layout_bytes;", "int64_t record_bytes;"}
	at := -1
	for _, field := range want {
		next := strings.Index(known, field)
		if next < 0 {
			t.Fatalf("TableFixedKnownLayout is missing %q: %s", field, known)
		}
		if next <= at {
			t.Fatalf("TableFixedKnownLayout's %q is not in order: %s", field, known)
		}
		at = next
	}

	report := t02Struct(t, text, "TableReport")
	trimmed := strings.TrimSpace(report)
	if !strings.HasSuffix(trimmed, "uint64_t layout_hash;") {
		t.Fatalf("TableReport's layout_hash is not its last member: %s", report)
	}

	if n := strings.Count(text, "layout_hash = "); n != 1 {
		t.Fatalf("layout_hash is assigned %d times in the emitted unit, want 1 (table_fixed_refuse_hash): it is not zero on every other path", n)
	}
}

// t02R26: §5.9 #4/#8 and the emitted lineage build: an entry that does not
// build carries its OWN lane's reason — table_fixed_parse_layout's failure is
// layout_malformed, a compile that will not fit the declared capacity is
// plan_too_large — and the load returns that entry's reason as a REFUSAL, never
// a throw. Asserted on the emitted load lane and the emitted build lane.
func t02R26(t *testing.T) {
	snake := "field_append"
	files := t02Files(t, cReadSchema(t, "VNEW_"+snake), []string{cReadSchema(t, "VOLD_"+snake)})
	text := t02Text(files)

	if !strings.Contains(text, "made == -2 ) ? SCHEMA_TABLE_LAYOUT_MALFORMED : SCHEMA_TABLE_PLAN_TOO_LARGE") {
		t.Fatalf("the lineage build does not name an entry's own lane (layout_malformed / plan_too_large)")
	}
	if !strings.Contains(text, "fixed_lineage[pick].reason != 0 ) { report->refused = 1; report->reason = ") {
		t.Fatalf("the load does not refuse by the matched entry's own recorded reason")
	}
}

// t02W14: SPEC-TABLES §3.4 and the emitted layout asserts: every number the
// plan and the destination rows rest on is checked against the compiler's own
// offsetof/sizeof at build time — the plan dst that a read lands through is the
// same byte offsetof names.
func t02W14(t *testing.T) {
	files := t02Files(t, cReadSchema(t, "VNEW_field_append"), nil)
	text := t02Text(files)

	if !regexp.MustCompile(`SCHEMA_TABLE_STATIC_ASSERT\( fixed_\w+_size, sizeof\( \w+ \) == \d+,`).MatchString(text) {
		t.Fatalf("no static assert ties a fixed plan's size to sizeof")
	}
	if !regexp.MustCompile(`SCHEMA_TABLE_STATIC_ASSERT\( fixed_\w+_\w+, offsetof\( \w+, \w+ \) == \d+,`).MatchString(text) {
		t.Fatalf("no static assert ties the plan dst to offsetof")
	}
}

// t02Struct returns the body of `typedef struct <name> { … } <name>;`.
func t02Struct(t *testing.T, text, name string) string {
	t.Helper()
	marker := "typedef struct " + name
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source carries no typedef struct %s", name)
	}
	rest := text[at:]
	end := strings.Index(rest, "} "+name+";")
	if end < 0 {
		t.Fatalf("typedef struct %s does not close", name)
	}
	return rest[:end]
}

// t02Number reads `<name> = <n>;` out of the generated source.
func t02Number(t *testing.T, text, name string) int64 {
	t.Helper()
	marker := name + " = "
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s", name)
	}
	rest := text[at+len(marker):]
	end := strings.IndexByte(rest, ';')
	if end < 0 {
		t.Fatalf("the published %s has no end", name)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
	if err != nil {
		t.Fatalf("the published %s %q is not a number: %v", name, rest[:end], err)
	}
	return n
}
