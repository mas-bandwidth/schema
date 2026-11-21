package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A JSON object's keys are held once each by a set, as Go's reader holds
// them in a map: every object the reader scans (a table's keys, a keyed
// array's keys, a skipped object's keys) tests each key as it is read, and
// no reader collects the keys to sort or scan them afterwards.
func TestLobsterJsonKeysUseTheSet(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "lobstertable", "runtime", "json.lobster"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []*regexp.Regexp{
		regexp.MustCompile(`sorted_keys\(`),
		regexp.MustCompile(`j_key_once\(`),
		regexp.MustCompile(`push\((keys|seen), key\)`),
		regexp.MustCompile(`let (keys|seen):\[string\] = \[\]`),
	} {
		if loc := banned.FindStringIndex(src); loc != nil {
			t.Errorf("json.lobster collects object keys for a later pass: %q", src[loc[0]:loc[1]])
		}
	}
	if n := strings.Count(src, "if not j_key_new(r, "); n != 4 {
		t.Errorf("json.lobster tests object keys with the set in %d readers, want 4 (table keys, keyed array, skipped object, map entries)", n)
	}
}

const jsonKeySetSchema = `package probe

enum Key { alpha, beta, gamma }

table Root
{
    need  int32
    keyed [Key]uint8
}
`

// A large object reads through the set: tens of thousands of distinct keys
// are admitted, and one repeat anywhere among them, in the table's own keys
// or in a skipped object's, is refused as Go refuses it.
func TestLobsterJsonLargeObjectKeys(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	path := writeSchema(t, "Probe", jsonKeySetSchema)
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	const n = 30000
	object := func(prefix string, extra ...string) string {
		var b strings.Builder
		b.WriteString("{")
		for i := range n {
			fmt.Fprintf(&b, `"%s%d":%d,`, prefix, i, i)
		}
		for _, e := range extra {
			b.WriteString(e + ",")
		}
		return strings.TrimSuffix(b.String(), ",") + "}"
	}
	rows := []struct {
		name, text string
		read       bool
	}{
		{"distinct_table_keys", strings.Replace(object("k"), "{", `{"need":1,`, 1), true},
		{"repeat_last_table_key", strings.Replace(object("k", `"k0":7`), "{", `{"need":1,`, 1), false},
		{"repeat_first_table_key", strings.Replace(object("k", `"need":2`), "{", `{"need":1,`, 1), false},
		{"distinct_skipped_keys", `{"need":1,"future":` + object("x") + `}`, true},
		{"repeat_skipped_key", `{"need":1,"future":` + object("x", fmt.Sprintf(`"x%d":0`, n/2)) + `}`, false},
		{"keyed_distinct", `{"need":1,"keyed":{"alpha":1,"beta":2,"gamma":3}}`, true},
		{"keyed_repeat", `{"need":1,"keyed":{"alpha":1,"beta":2,"alpha":3}}`, false},
		{"keyed_unknown_repeat", `{"need":1,"keyed":{"delta":1,"delta":2}}`, false},
		// the repeat is refused AT the repeated key, so damage after it is never reached
		{"repeat_then_damage", `{"need":1,"k":1,"k":2,`, false},
		{"skipped_repeat_then_damage", `{"need":1,"future":{"x":1,"x":2,`, false},
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString("import .json\n")
	inputs := map[string][]byte{}
	for i, row := range rows {
		var report tabletext.Report
		rec := m.New(m.Lookup("Root"))
		if got := m.Read(rec, []byte(row.text), &report) && !report.Refused; got != row.read {
			t.Fatalf("%s: Go read %t, want %t (%s)", row.name, got, row.read, report.Reason)
		}
		name := fmt.Sprintf("in%d.json", i)
		inputs[name] = []byte(row.text)
		fmt.Fprintf(&program, `def row%d():
    let data = read_file(%q)
    assert data
    let report = schema_rt.LoadReport {}
    let value = schema_rt.json_read("Root", data, report)
    print(%q + (if value: " read" else: " refused " + report.reason))
row%d()
`, i, name, row.name, i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "keys.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "keys.lobster", inputs))), "\n")
	if len(out) != len(rows) {
		t.Fatalf("got %d lines, want %d: %q", len(out), len(rows), out)
	}
	for i, row := range rows {
		want := row.name + " read"
		if !row.read {
			want = row.name + " refused duplicate_key"
		}
		if out[i] != want {
			t.Errorf("%s: %q, want %q", row.name, out[i], want)
		}
	}
}

const jsonMapOrderSchema = `package probe

table Root
{
    m map[string(8)]uint8
    n map[uint8]uint8
}
`

// A MAP'S REPEATED KEY IS REFUSED IN TEXT ORDER: the repeat is tested once
// its value is read and before the next entry is, as Go's readMap tests it,
// so a later entry's damage never changes the verdict. A valid map still
// lands in canonical key order whatever order the text spells.
func TestLobsterJsonMapRepeatRefusedInTextOrder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	path := writeSchema(t, "Probe", jsonMapOrderSchema)
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	rows := []struct {
		name, text, want string
		goRepeat         bool
	}{
		{"distinct", `{"m":{"b":2,"a":1,"c":3},"n":{"9":1,"2":2}}`, "read a,b,c 2,9", false},
		{"repeat", `{"m":{"a":1,"a":2}}`, "refused duplicate_key", true},
		{"wrong_value", `{"m":{"a":1,"b":"x"}}`, "refused kind_mismatch", false},
		{"repeat_then_wrong_value", `{"m":{"a":1,"a":2,"b":"x"}}`, "refused duplicate_key", true},
		{"repeat_then_true", `{"m":{"one":1,"one":2,"two":true}}`, "refused duplicate_key", true},
		{"wrong_value_then_repeat", `{"m":{"b":"x","a":1,"a":2}}`, "refused kind_mismatch", false},
		{"integer_repeat_then_wrong_value", `{"n":{"1":1,"1":2,"3":"x"}}`, "refused duplicate_key", true},
		{"repeat_then_damage", `{"m":{"a":1,"a":2,`, "refused duplicate_key", true},
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
def keys(rec:schema_rt.Record, at:int, text:bool) -> string:
    var out = ""
    for(rec.fields[at].entries) v, i:
        let t = v.tab
        assert t
        if i > 0: out += ","
        out += if text: t.fields[0].cell.s else: string(t.fields[0].cell.i)
    return out
`)
	inputs := map[string][]byte{}
	for i, row := range rows {
		var report tabletext.Report
		read := m.Read(m.New(m.Lookup("Root")), []byte(row.text), &report) && !report.Refused
		if read != strings.HasPrefix(row.want, "read") {
			t.Fatalf("%s: Go read %t (%s)", row.name, read, report.Reason)
		}
		if repeat := strings.Contains(report.Reason, "repeated map key"); repeat != row.goRepeat {
			t.Fatalf("%s: Go's reason %q, want a repeated map key: %t", row.name, report.Reason, row.goRepeat)
		}
		name := fmt.Sprintf("in%d.json", i)
		inputs[name] = []byte(row.text)
		fmt.Fprintf(&program, `def row%d():
    let data = read_file(%q)
    assert data
    let report = schema_rt.LoadReport {}
    let value = schema_rt.json_read("Root", data, report)
    print(if value: "read " + keys(value, 0, true) + " " + keys(value, 1, false) else: "refused " + report.reason)
row%d()
`, i, name, i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "maps.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "maps.lobster", inputs))), "\n")
	if len(out) != len(rows) {
		t.Fatalf("got %d lines, want %d: %q", len(out), len(rows), out)
	}
	for i, row := range rows {
		if out[i] != row.want {
			t.Errorf("%s: %q, want %q", row.name, out[i], row.want)
		}
	}
}
