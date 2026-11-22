package cstable

// A counted [Min..N] array of TABLE or UNION elements is at its BORN state
// when the live count is Min and every element writes nothing: a table
// element whose body is the terminator alone, a union element that is None.
// The writer elides it exactly then (docs/SPEC-TABLES.md §2.3, §4.6; R10), on
// the variable table's wire (the descriptor walk, Rides), on a fixed table's
// typed wire, and in the message form. A union arm holding a counted array is
// born at A elements, so selecting the arm holds A. The bytes are the
// independent engine's (internal/tablewire), the same in every language:
// born, one over the minimum and one element set must each match it.
//
// A FUNCTIONAL TEST: it compiles and runs C# with the leg's SDK. No clock,
// no socket, no network.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const csCountedMinSchema = `package probe

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

// one case: the form, the table, the engine's text for the value, and the C#
// lines that build the same value in `v` from a fresh one
type csCountedMinCase struct {
	id, form, table, text, build string
}

var csCountedMinCases = []csCountedMinCase{
	{"table_born", "wire", "R", `{"es":[{},{}]}`, ``},
	{"table_three", "wire", "R", `{"es":[{},{},{}]}`, `v.EsCount = 3;`},
	{"table_one_set", "wire", "R", `{"es":[{},{"n":7}]}`, `v.Es[1].N = 7;`},
	{"union_born", "wire", "RU", `{"us":[{},{}]}`, ``},
	{"union_three", "wire", "RU", `{"us":[{},{},{}]}`, `v.UsCount = 3;`},
	{"union_one_set", "wire", "RU", `{"us":[{},{"a":7}]}`, `v.Us[1].Type = Probe.UType.A; v.Us[1].A = 7;`},
	{"arm_selected", "wire", "RP", `{"p":{"few":[0,0]}}`, `v.P.Type = Probe.PType.Few;`},
	{"fixed_table_born", "wire", "FR", `{"es":[{},{}]}`, ``},
	{"fixed_table_three", "wire", "FR", `{"es":[{},{},{}]}`, `v.EsCount = 3;`},
	{"fixed_table_one_set", "wire", "FR", `{"es":[{},{"n":7}]}`, `v.Es[1].N = 7;`},
	{"fixed_union_born", "wire", "FRU", `{"us":[{},{}]}`, ``},
	{"fixed_union_one_set", "wire", "FRU", `{"us":[{},{"a":7}]}`, `v.Us[1].Type = Probe.UType.A; v.Us[1].A = 7;`},
	{"table_born", "message", "FR", `{"es":[{},{}]}`, ``},
	{"table_three", "message", "FR", `{"es":[{},{},{}]}`, `v.EsCount = 3;`},
	{"table_one_set", "message", "FR", `{"es":[{},{"n":7}]}`, `v.Es[1].N = 7;`},
	{"union_born", "message", "FRU", `{"us":[{},{}]}`, ``},
	{"union_three", "message", "FRU", `{"us":[{},{},{}]}`, `v.UsCount = 3;`},
	{"union_one_set", "message", "FRU", `{"us":[{},{"a":7}]}`, `v.Us[1].Type = Probe.UType.A; v.Us[1].A = 7;`},
}

func csCountedMinBody() string {
	var b strings.Builder
	for _, c := range csCountedMinCases {
		save := fmt.Sprintf("Probe.Schema.%sSave(v, buf)", c.table)
		if c.form == "message" {
			save = fmt.Sprintf("Probe.Schema.%sSaveMessages(new Probe.%s[] { v }, buf)", c.table, c.table)
		}
		fmt.Fprintf(&b, "        { var v = new Probe.%[3]s(); %[4]s byte[] buf = new byte[4096]; long n = %[5]s; Console.WriteLine(%[1]q + \" \" + %[2]q + \" \" + (n < 0 ? \"refused\" : Convert.ToHexString(buf, 0, (int)n).ToLowerInvariant())); }\n", c.form, c.id, c.table, c.build, save)
	}
	return b.String()
}

func TestCSharpCountedMinElementsElideAtBornState(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, csCountedMinSchema)
	m := tabletext.NewModel(u)
	want := map[string]string{}
	for _, c := range csCountedMinCases {
		v := m.New(m.Lookup(c.table))
		var rep tabletext.Report
		if !m.Read(v, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine did not read %s: %+v", c.id, c.text, rep)
		}
		var out []byte
		var err error
		if c.form == "wire" {
			out, err = tablewire.EncodeVariable(m, v) // the table wire, for a fixed table too
		} else {
			out, err = tablewire.EncodeMessages(m, []*tabletext.Instance{v})
		}
		if err != nil {
			t.Fatalf("%s %s: the engine did not encode: %v", c.form, c.id, err)
		}
		want[c.form+" "+c.id] = fmt.Sprintf("%x", out)
	}
	out := t08RunCS(t, csCountedMinSchema, csCountedMinBody())
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 3 && (parts[0] == "wire" || parts[0] == "message") {
			got[parts[0]+" "+parts[1]] = parts[2]
		}
	}
	for _, c := range csCountedMinCases {
		key := c.form + " " + c.id
		if got[key] != want[key] {
			t.Errorf("%s: C# wrote %q, the engine %s\n%s", key, got[key], want[key], out)
		}
	}
}
