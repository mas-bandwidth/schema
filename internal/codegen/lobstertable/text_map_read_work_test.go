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

// TestLobsterTextMapReadWorkIsBounded holds the readers' work bound in the
// number of map entries and object keys: each comparison the readers make is
// counted, once, at the entry of the ordering comparison and of the key-string
// comparison. Doubling the entries may not more than double the work (the
// factor allows a sort's constant), and N entries may not cost more than
// 4 N log2 N comparisons. Repeated keys must still refuse, and accepted map
// entries must still land ascending.
func TestLobsterTextMapReadWorkIsBounded(t *testing.T) {
	t.Parallel()
	const (
		schemaText = "package mapreadwork\ntable Root { m map[string]int32 }\n"
		small      = 2048
	)
	schema := filepath.Join(t.TempDir(), "MapReadWork.schema")
	if err := os.WriteFile(schema, []byte(schemaText), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	instrumentMapReadWork(t, g)

	for _, order := range []string{"ascending", "descending", "interleaved"} {
		n1, n2 := small, small*2
		c1 := runMapWork(t, g, mapJSONWorkProgram(g, order, n1), mapJSONInput(order, n1), "input.json")
		c2 := runMapWork(t, g, mapJSONWorkProgram(g, order, n2), mapJSONInput(order, n2), "input.json")
		checkMapWork(t, "JSON map "+order, small, c1, c2)
	}

	n1, n2 := small, small*2
	c1 := runMapWork(t, g, objectJSONWorkProgram(g), objectJSONInput(n1), "input.json")
	c2 := runMapWork(t, g, objectJSONWorkProgram(g), objectJSONInput(n2), "input.json")
	checkMapWork(t, "JSON object descending keys", small, c1, c2)

	c1 = runMapWork(t, g, textMapWorkProgram(g), textMapInput(n1), "input.txt")
	c2 = runMapWork(t, g, textMapWorkProgram(g), textMapInput(n2), "input.txt")
	checkMapWork(t, "text map descending", small, c1, c2)

	for _, form := range []string{"json", "text"} {
		if got := runMapWorkVerdict(t, g, form); got != "REFUSED" {
			t.Errorf("%s map with its first key repeated last: got %s, want REFUSED", form, got)
		}
	}

	for _, order := range []string{"ascending", "descending", "interleaved"} {
		if got := runMapWorkVerdictOrder(t, g, "json", order); got != "READ" {
			t.Errorf("JSON map %s without a repeat: got %s, want READ", order, got)
		}
	}
	if got := runMapWorkVerdictOrder(t, g, "text", "descending"); got != "READ" {
		t.Errorf("text map descending without a repeat: got %s, want READ", got)
	}
}

// instrumentMapReadWork plants one counter increment at the entry of every
// key comparison the readers use. map_key_order is the ordering comparison in
// both forms; JsonReader.key_order is the string ordering the object-key check
// uses. An object-key check that compares strings directly is counted there
// instead.
func instrumentMapReadWork(t *testing.T, g *loadUnit) {
	t.Helper()
	loadPath := filepath.Join(g.dir, "load.lobster")
	loadSrc, err := os.ReadFile(loadPath)
	if err != nil {
		t.Fatal(err)
	}
	load := string(loadSrc)
	const (
		loadAnchor  = "namespace schema_rt\n"
		orderAnchor = "def map_key_order(k:FieldDescriptor, a:Value, b:Value) -> int:\n"
	)
	if strings.Count(load, loadAnchor) != 1 {
		t.Fatalf("the counter's insertion point is in load.lobster %d times, want 1", strings.Count(load, loadAnchor))
	}
	load = strings.Replace(load, loadAnchor, loadAnchor+"\nlet j_work:[int] = [0]\n", 1)
	if strings.Count(load, orderAnchor) != 1 {
		t.Fatalf("map_key_order is in load.lobster %d times, want 1", strings.Count(load, orderAnchor))
	}
	load = strings.Replace(load, orderAnchor, orderAnchor+"    j_work[0]++\n", 1)
	if err := os.WriteFile(loadPath, []byte(load), 0o644); err != nil {
		t.Fatal(err)
	}

	jsonPath := filepath.Join(g.dir, "json.lobster")
	jsonSrc, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(jsonSrc)
	equality := false
	const (
		keyAnchor = "    def key_order(a:string, b:string) -> int:\n"
	)
	if n := strings.Count(jsonText, keyAnchor); n > 1 {
		t.Fatalf("JsonReader.key_order is in json.lobster %d times, want at most 1", n)
	} else if n == 1 {
		jsonText = strings.Replace(jsonText, keyAnchor, keyAnchor+"        j_work[0]++\n", 1)
		equality = true
	}
	if !equality {
		t.Fatal("no key-string comparison to count was found in json.lobster")
	}
	if err := os.WriteFile(jsonPath, []byte(jsonText), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checkMapWork(t *testing.T, shape string, n, countN, count2N int) {
	t.Helper()
	bound := 4.0 * float64(n) * math.Log2(float64(n))
	t.Logf("%s: N=%d count=%d, 2N=%d count=%d (N bound %d)", shape, n, countN, 2*n, count2N, int(bound))
	if float64(count2N) > 2.5*float64(countN) {
		t.Errorf("%s: count(2N)=%d is more than 2.5 x count(N)=%d", shape, count2N, countN)
	}
	if float64(countN) > bound {
		t.Errorf("%s: count(N)=%d is more than 4 N log2 N = %d", shape, countN, int(bound))
	}
}

func mapWorkKeys(n int, order string) []int {
	out := make([]int, 0, n)
	switch order {
	case "ascending":
		for i := range n {
			out = append(out, i)
		}
	case "descending":
		for i := n - 1; i >= 0; i-- {
			out = append(out, i)
		}
	case "interleaved":
		for i := 0; i < n; i += 2 {
			out = append(out, i)
		}
		for i := 1; i < n; i += 2 {
			out = append(out, i)
		}
	}
	return out
}

func mapWorkKey(i int) string { return fmt.Sprintf("k%05d", i) }

func mapJSONInput(order string, n int) []byte {
	var b strings.Builder
	b.WriteString(`{"m":{`)
	for j, i := range mapWorkKeys(n, order) {
		if j > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:%d", mapWorkKey(i), i)
	}
	b.WriteString(`}}`)
	return []byte(b.String())
}

func objectJSONInput(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"m":{},`)
	for j, i := range mapWorkKeys(n, "descending") {
		if j > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:0", mapWorkKey(i))
	}
	b.WriteString(`}`)
	return []byte(b.String())
}

func textMapInput(n int) []byte {
	var b strings.Builder
	b.WriteString(`(:m (`)
	for j, i := range mapWorkKeys(n, "descending") {
		if j > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "(%q %d)", mapWorkKey(i), i)
	}
	b.WriteString(`))`)
	return []byte(b.String())
}

func mapWorkImports(g *loadUnit) string {
	var b strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", mod)
	}
	return b.String()
}

func mapJSONWorkProgram(g *loadUnit, order string, n int) string {
	return mapWorkImports(g) + `import .json
let input = read_file("input.json")
assert input
let report = schema_rt.LoadReport {}
let rec = schema_rt.json_read("Root", input, report)
assert rec
let entries = rec.fields[0].entries
var i = 1
while i < length(entries):
    let a = entries[i - 1].tab
    let b = entries[i].tab
    assert a and b
    assert schema_rt.map_key_order(a.fields[0].fd, a.fields[0].cell, b.fields[0].cell) < 0
    i++
print(schema_rt.j_work[0])
`
}

func textMapWorkProgram(g *loadUnit) string {
	return mapWorkImports(g) + `import .text_form
let input = read_file("input.txt")
assert input
let doc = schema_rt.text_load("Root", input)
assert doc
let entries = doc.rec.fields[0].entries
var i = 1
while i < length(entries):
    let a = entries[i - 1].tab
    let b = entries[i].tab
    assert a and b
    assert schema_rt.map_key_order(a.fields[0].fd, a.fields[0].cell, b.fields[0].cell) < 0
    i++
print(schema_rt.j_work[0])
`
}

func objectJSONWorkProgram(g *loadUnit) string {
	return mapWorkImports(g) + `import .json
let input = read_file("input.json")
assert input
let rec = schema_rt.json_read("Root", input, schema_rt.LoadReport {})
assert rec
print(schema_rt.j_work[0])
`
}

func runMapWork(t *testing.T, g *loadUnit, program string, input []byte, name string) int {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{name: input})))
	n, err := strconv.Atoi(out)
	if err != nil {
		t.Fatalf("the work count is not a number: %q", out)
	}
	return n
}

func runMapWorkVerdict(t *testing.T, g *loadUnit, form string) string {
	t.Helper()
	dup := `{"m":{"k0":1,"k1":2,"k2":3,"k0":4}}`
	name := "input.json"
	if form == "text" {
		dup = `(:m (("k0" 1) ("k1" 2) ("k2" 3) ("k0" 4)))`
		name = "input.txt"
	}
	return runMapWorkVerdictInput(t, g, form, dup, name)
}

func runMapWorkVerdictOrder(t *testing.T, g *loadUnit, form, order string) string {
	t.Helper()
	const n = 4
	var b strings.Builder
	if form == "json" {
		b.WriteString(`{"m":{`)
		for j, i := range mapWorkKeys(n, order) {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%q:%d", mapWorkKey(i), i)
		}
		b.WriteString(`}}`)
		return runMapWorkVerdictInput(t, g, form, b.String(), "input.json")
	}
	b.WriteString(`(:m (`)
	for j, i := range mapWorkKeys(n, order) {
		if j > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "(%q %d)", mapWorkKey(i), i)
	}
	b.WriteString(`))`)
	return runMapWorkVerdictInput(t, g, form, b.String(), "input.txt")
}

func runMapWorkVerdictInput(t *testing.T, g *loadUnit, form, input, name string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(mapWorkImports(g))
	if form == "json" {
		b.WriteString(`import .json
let input = read_file("input.json")
assert input
let rec = schema_rt.json_read("Root", input, schema_rt.LoadReport {})
print(if rec: "READ" else: "REFUSED")
`)
	} else {
		b.WriteString(`import .text_form
let input = read_file("input.txt")
assert input
let doc = schema_rt.text_load("Root", input)
print(if doc: "READ" else: "REFUSED")
`)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{name: []byte(input)})))
}
