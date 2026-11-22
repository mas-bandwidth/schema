package cpptable

// A counted [Min..N] array of TABLE or UNION elements is at its BORN state
// when the live count is Min and every element writes nothing: a table
// element whose body is the terminator alone (judged with the id table rolled
// back), a union element that is None. A text list takes no minimum (the
// checker refuses [A..N]string(M), SPEC §2.13). The writer elides it exactly then
// (docs/SPEC-TABLES.md §2.3, §4.6). The bytes are the independent engine's
// (internal/tablewire), the same bytes in every language: born, one over the
// minimum, and one element set must each match it on the table wire and in
// the message form.
//
// The negative control plants the old judgement (a born array always rides)
// in the real artifact, and the born rows must then differ from the engine.
//
// A FUNCTIONAL TEST: it compiles and runs C++ with the leg's compiler. No
// clock, no socket, no network.

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const cppCountedMinElemsSchema = `package countedmin

fixed table E
{
    n uint32
}

union U
{
    a uint32
    b int32
}

fixed table R
{
    es [2..4]E
}

fixed table RU
{
    us [2..4]U
}
`

// one case: the table, the engine's text for the value, and the C++ lines
// that build the same value in `v` from a default-constructed one
type cppCountedMinCase struct {
	id, table, text, build string
}

var cppCountedMinCases = []cppCountedMinCase{
	{"table_born", "R", `{"es":[{},{}]}`, ``},
	{"table_three", "R", `{"es":[{},{},{}]}`, `v.es_count = 3;`},
	{"table_one_set", "R", `{"es":[{},{"n":7}]}`, `v.es[1].n = 7;`},
	{"union_born", "RU", `{"us":[{},{}]}`, ``},
	{"union_three", "RU", `{"us":[{},{},{}]}`, `v.us_count = 3;`},
	{"union_one_set", "RU", `{"us":[{},{"a":7}]}`, `v.us[1].type = UType::A; v.us[1].a = 7;`},
}

func cppCountedMinMain() string {
	var b strings.Builder
	b.WriteString(`
bool armmin_asserts_on = true;

static void hexout( const char * form, const char * id, const uint8_t * p, int64_t n )
{
    std::printf( "%s %s ", form, id );
    if ( n < 0 ) { std::printf( "refused\n" ); return; }
    for ( int64_t i = 0; i < n; ++i ) { std::printf( "%02x", (unsigned) p[i] ); }
    std::printf( "\n" );
}

int main()
{
    static uint8_t out[4096];
    TableReport r;
`)
	for _, c := range cppCountedMinCases {
		fmt.Fprintf(&b, `    {
        %[2]s v;
        %[3]s
        hexout( "wire", "%[1]s", out, %[2]sSave( v, out, (int64_t) sizeof( out ) ) );
        hexout( "message", "%[1]s", out, %[2]sSaveMessages( &v, 1, out, (int64_t) sizeof( out ), &r ) );
    }
`, c.id, c.table, c.build)
	}
	b.WriteString("    return 0;\n}\n")
	return b.String()
}

// cppCountedMinEngine is the engine's bytes for each case and form.
func cppCountedMinEngine(t *testing.T) map[string]string {
	t.Helper()
	u := cppUnitFrom(t, cppCountedMinElemsSchema)
	m := tabletext.NewModel(u)
	want := map[string]string{}
	for _, c := range cppCountedMinCases {
		v := m.New(m.Lookup(c.table))
		var rep tabletext.Report
		if !m.Read(v, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine did not read %s: %+v", c.id, c.text, rep)
		}
		wire, err := tablewire.EncodeVariable(m, v) // the table wire; Encode picks the fixed form for a fixed table
		if err != nil {
			t.Fatalf("%s: the engine did not encode: %v", c.id, err)
		}
		msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v})
		if err != nil {
			t.Fatalf("%s: the engine did not encode the message: %v", c.id, err)
		}
		want["wire "+c.id] = hex.EncodeToString(wire)
		want["message "+c.id] = hex.EncodeToString(msg)
	}
	return want
}

// cppCountedMinRun builds and runs the probe and returns form+id -> hex.
func cppCountedMinRun(t *testing.T, undermine func(map[string][]byte)) map[string]string {
	t.Helper()
	probe := cppProbeBuild(t, cppCountedMinElemsSchema, "countedmin", cppCountedMinMain(), undermine)
	code, out := cppArmMinExit(t, probe, "all")
	if code != 0 {
		t.Fatalf("the probe exited %d\n%s", code, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("an unexpected probe line: %q", line)
		}
		got[parts[0]+" "+parts[1]] = parts[2]
	}
	return got
}

func TestCppCountedMinElementsElideAtBornState(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	want := cppCountedMinEngine(t)
	got := cppCountedMinRun(t, nil)
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s: the C++ bytes differ from the engine's\n got %s\nwant %s", key, got[key], w)
		}
	}
}

func TestCppCountedMinElementsElideNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	want := cppCountedMinEngine(t)
	born := regexp.MustCompile(`(bool born_value_[a-z]+ = )`)
	got := cppCountedMinRun(t, func(files map[string][]byte) {
		planted := 0
		for name, data := range files {
			if text := born.ReplaceAllString(string(data), "${1}false && "); text != string(data) {
				files[name] = []byte(text)
				planted++
			}
		}
		if planted == 0 {
			t.Fatal("no generated file carried the birth judgement to plant out")
		}
	})
	for _, c := range cppCountedMinCases {
		if !strings.HasSuffix(c.id, "_born") {
			continue
		}
		for _, form := range []string{"wire", "message"} {
			if key := form + " " + c.id; got[key] == want[key] {
				t.Errorf("%s: with the birth judgement planted out, the C++ bytes still match the engine's", key)
			}
		}
	}
}
