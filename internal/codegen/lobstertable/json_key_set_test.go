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
	if n := strings.Count(src, "if not j_key_new(r, "); n != 3 {
		t.Errorf("json.lobster tests object keys with the set in %d readers, want 3 (table keys, keyed array, skipped object)", n)
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
