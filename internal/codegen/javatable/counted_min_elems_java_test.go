package javatable

// A counted [Min..N] array is at its BORN state when the live count is Min and
// every element writes nothing: a table element whose body is the terminator
// alone (judged with the id table rolled back), a union element that is None,
// a scalar at its default. A Java value is born there (A default elements, in
// a table and in a union arm alike), and the writer elides it exactly then
// (docs/SPEC-TABLES.md §2.3, §4.6; R10). The bytes are the independent
// engine's (internal/tablewire), the same in every language: born, one over
// the minimum and one element set must each match it on the variable wire
// (tables R, RU, RP) and in the message form (fixed tables FR, FRU).
//
// A FUNCTIONAL TEST: it compiles and runs Java with the leg's JDK. No clock,
// no socket, no network.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const javaCountedMinSchema = `package probe

table E
{
    n uint32
}

union U
{
    a uint32
    b int32
}

union P
{
    few [2..4]uint32
    other int32
}

table R
{
    es [2..4]E
}

table RU
{
    us [2..4]U
}

table RP
{
    p P
}

fixed table FE
{
    n uint32
}

fixed table FR
{
    es [2..4]FE
}

fixed table FRU
{
    us [2..4]U
}
`

// one case: the form, the table, the engine's text for the value, and the
// Java lines that build the same value in `v` from a fresh one
type javaCountedMinCase struct {
	id, form, table, text, build string
}

var javaCountedMinCases = []javaCountedMinCase{
	{"table_born", "wire", "R", `{"es":[{},{}]}`, ``},
	{"table_three", "wire", "R", `{"es":[{},{},{}]}`, `v.es.add(new EVariable.Value());`},
	{"table_one_set", "wire", "R", `{"es":[{},{"n":7}]}`, `v.es.get(1).n = 7;`},
	{"union_born", "wire", "RU", `{"us":[{},{}]}`, ``},
	{"union_three", "wire", "RU", `{"us":[{},{},{}]}`, `v.us.add(new UVariable.Value());`},
	{"union_one_set", "wire", "RU", `{"us":[{},{"a":7}]}`, `v.us.get(1).tag = 1; v.us.get(1).a = 7;`},
	{"arm_selected", "wire", "RP", `{"p":{"few":[0,0]}}`, `v.p.tag = 1;`},
	{"arm_one_set", "wire", "RP", `{"p":{"few":[7,0]}}`, `v.p.tag = 1; v.p.few.set(0, 7);`},
	{"table_born", "message", "FR", `{"es":[{},{}]}`, ``},
	{"table_three", "message", "FR", `{"es":[{},{},{}]}`, `v.esCount = 3;`},
	{"table_one_set", "message", "FR", `{"es":[{},{"n":7}]}`, `v.es[1].n = 7;`},
	{"union_born", "message", "FRU", `{"us":[{},{}]}`, ``},
	{"union_three", "message", "FRU", `{"us":[{},{},{}]}`, `v.usCount = 3;`},
	{"union_one_set", "message", "FRU", `{"us":[{},{"a":7}]}`, `v.us[1].type = 1; v.us[1].a = 7;`},
}

func javaCountedMinDriver() string {
	var b strings.Builder
	b.WriteString(`package probe;

public final class Driver {
    static void hexout(String form, String id, byte[] p, int n) {
        StringBuilder s = new StringBuilder(form + " " + id + " ");
        if (n < 0) { s.append("refused"); }
        for (int i = 0; i < n; i++) { s.append(String.format("%02x", p[i] & 0xff)); }
        System.out.println(s);
    }

    public static void main(String[] args) {
`)
	for _, c := range javaCountedMinCases {
		if c.form == "wire" {
			fmt.Fprintf(&b, "        { %[2]sVariable.Value v = new %[2]sVariable.Value(); %[3]s byte[] out = %[2]sVariable.save(v); hexout(\"wire\", %[1]q, out, out == null ? -1 : out.length); }\n", c.id, c.table, c.build)
		} else {
			fmt.Fprintf(&b, "        { %[2]sFixed.Value v = new %[2]sFixed.Value(); %[3]s byte[] out = new byte[4096]; int n = %[2]sMessage.saveMessages(new %[2]sFixed.Value[] { v }, 1, out, new TableVocabulary.Report()); hexout(\"message\", %[1]q, out, n); }\n", c.id, c.table, c.build)
		}
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

func TestJavaCountedMinElementsElideAtBornState(t *testing.T) {
	t.Parallel()
	javac, java := javaTools(t)
	u := unitFrom(t, javaCountedMinSchema)
	m := tabletext.NewModel(u)
	want := map[string]string{}
	for _, c := range javaCountedMinCases {
		v := m.New(m.Lookup(c.table))
		var rep tabletext.Report
		if !m.Read(v, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine did not read %s: %+v", c.id, c.text, rep)
		}
		var out []byte
		var err error
		if c.form == "wire" {
			out, err = tablewire.EncodeVariable(m, v)
		} else {
			out, err = tablewire.EncodeMessages(m, []*tabletext.Instance{v})
		}
		if err != nil {
			t.Fatalf("%s %s: the engine did not encode: %v", c.form, c.id, err)
		}
		want[c.form+" "+c.id] = hexOf(out)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
	files["Driver.java"] = []byte(javaCountedMinDriver())
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, java, "-ea", "-cp", dir, u.Package+".Driver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("an unexpected probe line: %q\n%s", line, out)
		}
		got[parts[0]+" "+parts[1]] = parts[2]
	}
	for _, c := range javaCountedMinCases {
		key := c.form + " " + c.id
		if got[key] != want[key] {
			t.Errorf("%s: Java wrote %s, the engine %s", key, got[key], want[key])
		}
	}
}
