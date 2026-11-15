package lobstertable_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// HOSTILE INPUT COSTS BOUNDED TIME (docs/SECURITY.md; docs/SPEC-TABLES.md
// §16): the JSON and text readers take a map, and an object's keys, in work
// O(n log n) in the number of entries, whatever order the keys arrive in,
// while a repeated key is still refused and the entries still land in
// ascending key order.
//
// The gate never measures time. It copies the generated unit and plants one
// counter increment at the entry of every key comparison the readers use:
// map_key_order (the ordering comparison every map-entry order goes
// through), key_order inside j_key_once (the object-key comparison) and, where a
// reader still scans earlier keys, the key-string equality test inside that
// scan. Each shape is read at N and 2N entries and must hold
// count(2N) <= 2.5 x count(N) and count(N) <= 4 x N x log2(N).

const mapWorkSchema = "package mapworkprobe\ntable Root { m map[int32]int32 }\n"

const mapWorkN = 2048

// mapWorkPlants are the comparison entries the counter is planted at: the
// anchor, and what replaces it. The ordering comparison must be present; the
// key-string comparisons are planted wherever they are.
var mapWorkPlants = []struct {
	file, anchor, planted string
	required              bool
}{
	{"load.lobster", "def map_key_order(k:FieldDescriptor, a:Value, b:Value) -> int:\n", "def map_key_order(k:FieldDescriptor, a:Value, b:Value) -> int:\n    key_ops.n++\n", true},
	{"json.lobster", "    def key_order(a:string, b:string) -> int:\n", "    def key_order(a:string, b:string) -> int:\n        key_ops.n++\n", false},
	{"json.lobster", "    for(keys) prior:\n        if prior == key:\n", "    for(keys) prior:\n        key_ops.n++\n        if prior == key:\n", false},
}

const mapWorkProgram = `import .json
import .text_form
def ascending(rec:schema_rt.Record) -> bool:
    for(rec.fields) sl:
        var last = 0
        for(sl.entries) e, k:
            let t = e.tab
            if not t:
                return false
            let key = t.fields[0].cell.i
            if k > 0 and key <= last:
                return false
            last = key
    return true
def probe(name:string, form:string):
    let input = read_file(name)
    assert input
    schema_rt.key_ops.n = 0
    var verdict = "refused"
    var order = "-"
    if form == "json":
        let rec = schema_rt.json_read("Root", input, schema_rt.LoadReport {})
        if rec:
            verdict = "read"
            order = if ascending(rec): "ascending" else: "unordered"
    else:
        let doc = schema_rt.text_load("Root", input)
        if doc:
            verdict = "read"
            order = if ascending(doc.rec): "ascending" else: "unordered"
    print(name + " " + verdict + " " + order + " " + string(schema_rt.key_ops.n))
`

// mapWorkKeys is n keys in one of three arrival orders.
func mapWorkKeys(n int, order string) []int {
	keys := make([]int, 0, n)
	switch order {
	case "ascending":
		for i := range n {
			keys = append(keys, i)
		}
	case "descending":
		for i := n - 1; i >= 0; i-- {
			keys = append(keys, i)
		}
	case "interleaved":
		for i := range n / 2 {
			keys = append(keys, i, n-1-i)
		}
		if n%2 == 1 {
			keys = append(keys, n/2)
		}
	}
	return keys
}

func mapWorkJSONMap(keys []int) string {
	var b strings.Builder
	b.WriteString(`{"m":{`)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"%d":%d`, k, i)
	}
	b.WriteString("}}")
	return b.String()
}

// mapWorkJSONObject is an object of n unknown keys, in descending order.
func mapWorkJSONObject(n int, repeat bool) string {
	var b strings.Builder
	b.WriteString("{")
	for i := n - 1; i >= 0; i-- {
		fmt.Fprintf(&b, `"u%05d":%d,`, i, i)
	}
	if repeat {
		fmt.Fprintf(&b, `"u%05d":0,`, n-1)
	}
	b.WriteString(`"m":{}}`)
	return b.String()
}

func mapWorkText(keys []int) string {
	var b strings.Builder
	b.WriteString("(:m (")
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "(%d %d)", k, i)
	}
	b.WriteString("))")
	return b.String()
}

// TestLobsterTextMapReadWorkIsBounded holds the readers' map and object-key
// work to O(n log n) by operation count, and keeps the repeat refusal and the
// ascending order.
func TestLobsterTextMapReadWorkIsBounded(t *testing.T) {
	t.Parallel()
	schema := filepath.Join(t.TempDir(), "MapWork.schema")
	if err := os.WriteFile(schema, []byte(mapWorkSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	planted := map[string]int{}
	for _, p := range mapWorkPlants {
		path := filepath.Join(g.dir, p.file)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		n := strings.Count(src, p.anchor)
		if p.required && n != 1 {
			t.Fatalf("the counter's anchor %q is in %s %d times, not once", p.anchor, p.file, n)
		}
		if n == 0 {
			continue
		}
		if p.file == "load.lobster" {
			const ns = "namespace schema_rt\n"
			if strings.Count(src, ns) < 1 {
				t.Fatal("load.lobster opens no namespace schema_rt")
			}
			src = strings.Replace(src, ns, ns+"class KeyOps:\n    n:int = 0\nlet key_ops = KeyOps {}\n", 1)
		}
		src = strings.ReplaceAll(src, p.anchor, p.planted)
		planted[p.file+":"+strings.TrimSpace(strings.SplitN(p.anchor, "\n", 2)[0])] += n
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(planted) < 2 {
		t.Fatalf("the counter is planted at %v: the object-key comparison is not planted, so the gate is vacuous", planted)
	}

	type shape struct{ name, form string }
	shapes := []shape{
		{"json-map-ascending", "json"},
		{"json-map-descending", "json"},
		{"json-map-interleaved", "json"},
		{"json-object", "json"},
		{"text-map-descending", "text"},
	}
	inputs := map[string][]byte{}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(mapWorkProgram)
	add := func(name, form, input string) {
		inputs[name] = []byte(input)
		fmt.Fprintf(&b, "probe(%q, %q)\n", name, form)
	}
	for _, s := range shapes {
		for _, n := range []int{mapWorkN, 2 * mapWorkN} {
			name := s.name + "-" + strconv.Itoa(n)
			switch {
			case strings.HasPrefix(s.name, "json-map-"):
				add(name, s.form, mapWorkJSONMap(mapWorkKeys(n, strings.TrimPrefix(s.name, "json-map-"))))
			case s.name == "json-object":
				add(name, s.form, mapWorkJSONObject(n, false))
			default:
				add(name, s.form, mapWorkText(mapWorkKeys(n, "descending")))
			}
		}
	}
	// one key repeated, first and last: still refused in both forms
	repeated := append(mapWorkKeys(mapWorkN, "descending"), mapWorkN-1)
	add("json-map-repeat", "json", mapWorkJSONMap(repeated))
	add("json-object-repeat", "json", mapWorkJSONObject(mapWorkN, true))
	add("text-map-repeat", "text", mapWorkText(repeated))
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := map[string][3]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("not a probe answer: %q", line)
		}
		got[f[0]] = [3]string{f[1], f[2], f[3]}
	}
	if len(got) != len(inputs) {
		t.Fatalf("%d answers for %d probes", len(got), len(inputs))
	}
	for _, name := range []string{"json-map-repeat", "json-object-repeat", "text-map-repeat"} {
		if got[name][0] != "refused" {
			t.Errorf("%s: a repeated key is %s, not refused", name, got[name][0])
		}
	}
	bound := 4 * float64(mapWorkN) * math.Log2(mapWorkN)
	for _, s := range shapes {
		at := func(n int) int {
			r := got[s.name+"-"+strconv.Itoa(n)]
			if r[0] != "read" {
				t.Fatalf("%s at %d entries is %s", s.name, n, r[0])
			}
			if r[1] != "ascending" {
				t.Errorf("%s at %d entries: the entries are not in ascending key order", s.name, n)
			}
			c, err := strconv.Atoi(r[2])
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
		small, large := at(mapWorkN), at(2*mapWorkN)
		t.Logf("%s: %d key comparisons at N=%d, %d at 2N", s.name, small, mapWorkN, large)
		if small == 0 {
			t.Errorf("%s: no key comparison was counted: the gate is vacuous", s.name)
			continue
		}
		if float64(large) > 2.5*float64(small) {
			t.Errorf("%s: %d comparisons at 2N is more than 2.5 x %d at N: the read is not O(n log n)", s.name, large, small)
		}
		if float64(small) > bound {
			t.Errorf("%s: %d comparisons at N=%d is more than 4 x N x log2(N) = %.0f", s.name, small, mapWorkN, bound)
		}
	}
}
