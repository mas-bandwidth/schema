package goosetable

// A TEXT LIST'S TOTAL (docs/SPEC-TABLES.md §2.13) in the Goose runtime, both
// forms over the one FieldDesc fact (text_total) tablewire's descriptors
// carry: the read keeps the longest prefix whose bytes are within the total
// and drops the rest, clamped counted once; an element past M clamps first
// and counts on its own; a count past [..N] is clamped before the total meets
// it; and the write refuses a value past the total, the text form's -1 with
// nothing appended and the wire's invalid_value. The text read names the
// field in its report's clamped-field slot, `clamped: <Table>.<field>`
// (§6.5). The answers are internal/tabletext's (KeepTextTotal,
// ValidateTextTotal) and gotable's wire read.
//
// Each probe is one text: the text read reads it, and the WIRE read reads the
// bytes the engine packs from it under the same unit with no total and a
// wider M, the writer a reader with the total meets (§2.13: the total moves no
// wire byte).
//
// TestGooseTextTotalNegativeControl takes the total out of the unit's runtime,
// the text read, the wire load and both writes, and requires exactly the
// probes that meet it red.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// textTotalProbe is one text, the counters both reads owe, the clamped-field
// name the text read gives ("-" for none) and the value both reads keep, as
// its compacted text.
type textTotalProbe struct{ name, input, verdict, named, text string }

var textTotalProbes = []textTotalProbe{
	// the prefix: "abc" would carry 6 to 9, so it drops and "a" with it
	{"prefix", `{"rules": ["abcdef", "abc", "a"]}`, "0,0,0,1,0", "clamped: Handover.rules", `{"rules":["abcdef"],"first":[]}`},
	// exactly the total: kept whole, clean
	{"at", `{"rules": ["abcd", "abcd"]}`, "0,0,0,0,0", "-", `{"rules":["abcd","abcd"],"first":[]}`},
	// the element clamps at M first (counted), then the total drops "a"
	{"clamp", `{"rules": ["abcdefghij", "a"]}`, "0,0,0,2,0", "clamped: Handover.rules", `{"rules":["abcdefgh"],"first":[]}`},
	// a count past [..4] is clamped, and the kept four are within the total
	{"count", `{"rules": ["a", "b", "c", "d", "e"]}`, "0,0,0,1,0", "-", `{"rules":["a","b","c","d"],"first":[]}`},
	// an absent element counts zero; "abc" would carry 2 to 5
	{"optional", `{"first": [null, "ab", null, "abc", "a"]}`, "0,0,0,1,0", "clamped: Handover.first", `{"rules":[],"first":[null,"ab",null]}`},
}

// textTotalPast is what the writes answer for a value past its total: the
// text form -1 and the one byte the buffer held before it, the wire
// invalid_value (SaveRefuseInvalidValue) and that same one byte.
const textTotalPast = "-1 1 8 1"

// The clauses that hold the total in the runtime, which the negative control
// takes out: the text read's and write's, the wire load's and save's.
var textTotalClauses = []struct{ file, clause, broken string }{
	{"json_value.goose", "            if total > 0 && n > total - spent {", "            if total < 0 && n > total - spent {"},
	{"json_value.goose", "        if sum > total { return false; }", "        if sum < 0 { return false; }"},
	{"wire_load.goose", "                if d.text_total > 0 {\n                    let es", "                if d.text_total < 0 {\n                    let es"},
	{"wire_save.goose", "    if d.text_total > 0 && text_sum(c, s, count) > d.text_total {", "    if d.text_total < 0 && text_sum(c, s, count) > d.text_total {"},
}

const textTotalSchema = "package texttotal\n\ntable Handover\n{\n    rules [..4]string(8) | total = 8\n    first []?string(8) | total = 4\n}\n"

// the writer's view of the same table: no total, a wider M and count
const textTotalWriter = "package texttotal\n\ntable Handover\n{\n    rules [..8]string(16)\n    first []?string(16)\n}\n"

func TestGooseTextTotal(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs both forms of the variable class)")
	got, past := runTextTotal(t, nil)
	for _, w := range textTotalProbes {
		for _, form := range []string{"text", "wire"} {
			want := w.verdict + " " + w.text
			if form == "text" {
				want = w.verdict + " " + w.named + " " + w.text
			}
			if got[form+" "+w.name] != want {
				t.Errorf("%s %s: got %s, want %s", form, w.name, got[form+" "+w.name], want)
			}
		}
	}
	if past != textTotalPast {
		t.Errorf("a value past its total: the writes answered %q, want %q (each refused, nothing appended)", past, textTotalPast)
	}
}

func TestGooseTextTotalNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs both forms of the variable class)")
	got, past := runTextTotal(t, func(files map[string][]byte) {
		for _, c := range textTotalClauses {
			src := string(files[c.file])
			if strings.Count(src, c.clause) != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the clause %q must occur exactly once in %s", c.clause, c.file)
			}
			files[c.file] = []byte(strings.Replace(src, c.clause, c.broken, 1))
		}
	})
	var red []string
	for _, w := range textTotalProbes {
		for _, form := range []string{"text", "wire"} {
			want := w.verdict + " " + w.text
			if form == "text" {
				want = w.verdict + " " + w.named + " " + w.text
			}
			if got[form+" "+w.name] != want {
				red = append(red, form+" "+w.name)
			}
		}
	}
	// every probe whose value meets the total, in both forms, and not the
	// two that stay within it (count is held by the count's bound alone)
	want := []string{"text prefix", "wire prefix", "text clamp", "wire clamp", "text optional", "wire optional"}
	if !slices.Equal(red, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the total taken out turned %v red, want exactly %v", red, want)
	}
	if past == textTotalPast {
		t.Fatalf("NEGATIVE CONTROL FAILED: the writes' total taken out still refused a value past it")
	}
	t.Logf("text total negative control: red on exactly %v, and the writes answered %q", red, past)
}

// runTextTotal generates the unit, applies sabotage to its files when it is
// not nil, and runs the probes. It answers, keyed `text <probe>` and
// `wire <probe>`, each read's `<counters> [<named>] <kept value>`, and what
// the two writes answered for a value past its total.
func runTextTotal(t *testing.T, sabotage func(map[string][]byte)) (map[string]string, string) {
	t.Helper()
	src := t.TempDir()
	load := func(name, text string) *tabletext.Model {
		path := filepath.Join(src, name, "Total.schema")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		u, err := loadSchemaPaths([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		return tabletext.NewModel(u)
	}
	reader := load("reader", textTotalSchema)
	writer := load("writer", textTotalWriter)
	u := reader.Unit
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	carried := false
	for _, data := range files {
		carried = carried || strings.Contains(string(data), "text_total: 8")
	}
	if !carried {
		t.Fatal("the descriptors carry no text_total: 8 for Handover.rules")
	}
	if sabotage != nil {
		sabotage(files)
	}
	inputs := map[string][]byte{}
	for _, p := range textTotalProbes {
		inputs[p.name+".json"] = []byte(p.input)
		tree := filepath.Join(src, "tree-"+p.name)
		if err := os.MkdirAll(tree, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tree, "Handover.json"), []byte(p.input), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, _, report, err := tablepack.Pack(writer, "Handover", tree)
		if err != nil || report.Refused {
			t.Fatalf("%s: the writer's pack: %v %+v", p.name, err, report)
		}
		inputs[p.name+".bin"] = wire
	}
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	slices.Sort(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		b.WriteString("import ." + m + ";\n")
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString(strings.ReplaceAll(`// text reads one text tolerantly and prints its counters, the field its
// clamped-field slot names, and the text the value writes back
fn text(name: const u8[:]) {
    var text: u8[>..] = [];
    if !read_file(name, text) { print("read-failed"); return; }
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::json_report();
    let at = schema_rt::json_load_table(NS::TableUnit, NS::TextUnit, "Handover", text, data, vals, nodes, r);
    if at < 0 || r.malformed { print("refused"); print("-"); print(""); return; }
    print(r.unknown, ",", r.kind_mismatch, ",", r.widened, ",", r.clamped, ",", r.duplicate);
    var why: u8[>..] = [];
    if schema_rt::json_clamped_reason(r, why) { print(why); } else { print("-"); }
    var out: u8[>..] = [];
    if schema_rt::json_save_table(NS::TableUnit, NS::TextUnit, "Handover", data, vals, nodes, at, out) != out.len { print("short"); }
    hex(out);
}

// wire loads one file tolerantly and prints its counters and the text the
// value writes back
fn wire(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(name, data) { print("read-failed"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(NS::TableUnit, "Handover", data, vals, nodes, r);
    if at < 0 || r.malformed || r.refused { print("refused"); print(""); return; }
    print(r.unknown, ",", r.kind_mismatch, ",", r.widened, ",", r.clamped, ",", r.duplicate);
    var out: u8[>..] = [];
    if schema_rt::json_save_table(NS::TableUnit, NS::TextUnit, "Handover", data, vals, nodes, at, out) != out.len { print("short"); }
    hex(out);
}

// past moves one kept element of rules to five bytes, carrying the sum past
// the total, and prints what each write answers and how much it appended
fn past(name: const u8[:]) {
    var text: u8[>..] = [];
    if !read_file(name, text) { print("read-failed"); return; }
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::json_report();
    let at = schema_rt::json_load_table(NS::TableUnit, NS::TextUnit, "Handover", text, data, vals, nodes, r);
    if at < 0 || r.malformed { print("refused"); return; }
    let list = vals[at].first;
    let rec = vals[list].first + 1;
    let fs = vals[rec].first;
    let off = data.len;
    data.append("abcde");
    vals[fs].off = off;
    vals[fs].len = 5;
    var out: u8[>..] = ['x'];
    let t = schema_rt::json_save_table(NS::TableUnit, NS::TextUnit, "Handover", data, vals, nodes, at, out);
    let tn = out.len;
    var file: u8[>..<] = ['x'];
    let w = schema_rt::save_table(NS::TableUnit, "Handover", data, vals, nodes, at, file);
    print(t, " ", tn, " ", w, " ", file.len);
}

fn main() {
`, "NS", goose.Namespace(u)))
	for _, p := range textTotalProbes {
		fmt.Fprintf(&b, "    text(%q);\n    wire(%q);\n", p.name+".json", p.name+".bin")
	}
	b.WriteString("    past(\"at.json\");\n}\n")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", inputs))), "\n")
	compact := func(line string) string {
		raw, err := hex.DecodeString(line)
		if err != nil {
			return "not-hex:" + line
		}
		return strings.Join(strings.Fields(string(raw)), "")
	}
	got := map[string]string{}
	at := 0
	next := func() string {
		if at >= len(lines) {
			t.Fatalf("the probe printed %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
		}
		at++
		return lines[at-1]
	}
	for _, p := range textTotalProbes {
		verdict, named := next(), next()
		got["text "+p.name] = verdict + " " + named + " " + compact(next())
		verdict = next()
		got["wire "+p.name] = verdict + " " + compact(next())
	}
	past := next()
	if at != len(lines) {
		t.Fatalf("the probe printed %d lines, read %d:\n%s", len(lines), at, strings.Join(lines, "\n"))
	}
	return got, past
}
