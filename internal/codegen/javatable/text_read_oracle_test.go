package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE JAVA TEXT READ AGREES WITH THE GO ENGINE on the shapes the shared
// hostile corpus (internal/ci/hostilecorpus) holds it to, case by case: the
// verdict, and for an accepted text its canonical text (toJson) and its file
// save byte for byte.
//
//   - A [A..B]T list is BORN AT A (docs/SPEC.md §4.2): a fresh value and an
//     absent field hold A value-initialized elements, a placed list replaces
//     them whole, and the save elides a list at its birth value as Go does.
//   - A LABEL (docs/SPEC-TABLES.md §16.7) alone, a trailing comma allowed,
//     refers; in a skipped value of a variable root the label rules hold
//     (first key, one spelling, a reference names a closed definition, a
//     definition is new); a fixed root skips such a value whole.
//   - DEPTH (§16.2): a union's arm value sits two levels below its holder and
//     a map's entry value one level below the map, so the text one level past
//     the cap refuses there as the engine's does and the text at the cap reads.
const textOracleSchema = `package probe

type Inner { x int32 }

table Leaf { v int32 }

table Node
{
    value int32
    next  *Node
}

union Pick
{
    leaf Leaf
    n    int32
}

table Probe
{
    items  [2..4]int32
    inner  [1..3]Inner
    pick   Pick
    roster map[string(8)]Leaf
    head   *Node
    alias  *Node
}

fixed table Flat
{
    a int32
}
`

type textOracleCase struct {
	name, root, text string
}

func nested(open, close string, n int) string {
	return strings.Repeat(open, n) + strings.Repeat(close, n)
}

func textOracleCases() []textOracleCase {
	cases := []textOracleCase{
		{"born-defaults", "Probe", `{}`},
		{"born-replaced", "Probe", `{"items": [7, 8, 9], "inner": [{"x": 1}, {}]}`},
		{"born-at-birth-value", "Probe", `{"items": [0, 0], "inner": [{}]}`},
		{"born-off-birth-value", "Probe", `{"items": [0, 1], "inner": [{"x": 0}]}`},
		{"born-below-min", "Probe", `{"items": [7]}`},
		{"label-reference-trailing-comma", "Probe", `{"head": {"&node": 1, "value": 5}, "alias": {"&node": 1,}}`},
		{"label-reference-trailing-comma-then-field", "Probe", `{"head": {"&node": 1, "value": 5}, "alias": {"&node": 1, , "value": 2}}`},
		{"skipped-label-defined-twice", "Probe", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1, "value": 2}}`},
		{"skipped-label-misspelled", "Probe", `{"zz": {"&foo": 1}}`},
		{"skipped-label-not-first", "Probe", `{"zz": {"value": 1, "&node": 2}}`},
		{"skipped-reference-undefined", "Probe", `{"zz": {"&node": 5}}`},
		{"skipped-reference-inside-own-definition", "Probe", `{"zz": {"&node": 5, "value": {"&node": 5}}}`},
		{"skipped-plain-object", "Probe", `{"zz": {"value": 1}, "head": {"&node": 5, "value": 1}}`},
		{"skipped-definition-then-reference", "Probe", `{"zz": {"&node": 5, "value": 1}, "head": {"&node": 5}}`},
		{"skipped-reference-trailing-comma", "Probe", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1,}}`},
		{"fixed-root-skips-label-whole", "Flat", `{"zz": {"&node": 1}}`},
		{"fixed-root-skips-misspelled-whole", "Flat", `{"zz": {"value": 1, "&foo": 1}}`},
	}
	// Each depth shape is walked across the cap, so the case one level past
	// it and the case at it are both here, wherever the engine puts them.
	for n := 120; n <= 132; n++ {
		cases = append(cases,
			textOracleCase{fmt.Sprintf("depth-body-arm-%d", n), "Probe", `{"pick": {"leaf": {"zz": ` + nested("[", "]", n) + `}}}`},
			textOracleCase{fmt.Sprintf("depth-unknown-arm-%d", n), "Probe", `{"pick": {"nope": ` + nested("[", "]", n) + `}}`},
			textOracleCase{fmt.Sprintf("depth-map-value-%d", n), "Probe", `{"roster": {"a": {"zz": ` + nested("[", "]", n) + `}}}`},
		)
	}
	return cases
}

func TestJavaTextReadAgreesWithEngine(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, textOracleSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	cases := textOracleCases()
	want := make([]string, len(cases))
	for i, c := range cases {
		inst := m.New(m.Lookup(c.root))
		var rep tabletext.Report
		if !m.Read(inst, []byte(c.text), &rep) || rep.Refused {
			want[i] = "refuse"
			continue
		}
		text, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: oracle write: %v", c.name, err)
		}
		// the Java writer ends its text with a newline (the corpus digests
		// the same bytes)
		text = append(text, '\n')
		if c.root != "Probe" {
			want[i] = "accept " + hexOf(text)
			continue
		}
		wire, err := tablewire.EncodeVariable(m, inst)
		if err != nil {
			t.Fatalf("%s: oracle encode: %v", c.name, err)
		}
		want[i] = "accept " + hexOf(text) + " " + hexOf(wire)
	}
	// the cases the corpus names, held to the verdict the engine gives them,
	// so the oracle walk cannot drift away from what this test is for
	expect := map[string]string{
		"born-below-min":                 "refuse",
		"label-reference-trailing-comma": "accept",
		"skipped-label-defined-twice":    "refuse",
		"skipped-label-misspelled":       "refuse",
		"skipped-label-not-first":        "refuse",
		"skipped-reference-undefined":    "refuse",
		"skipped-plain-object":           "accept",
		"fixed-root-skips-label-whole":   "accept",
	}
	flips := map[string]bool{}
	for i, c := range cases {
		verdict, _, _ := strings.Cut(want[i], " ")
		if e, ok := expect[c.name]; ok && e != verdict {
			t.Fatalf("%s: the engine says %s, this test expects %s", c.name, verdict, e)
		}
		if shape, n, ok := depthCase(c.name); ok && verdict == "refuse" && !flips[shape] {
			flips[shape] = true
			if n == 120 {
				t.Fatalf("%s: the engine refuses the shallowest case; widen the walk", c.name)
			}
		}
	}
	for _, shape := range []string{"depth-body-arm", "depth-unknown-arm", "depth-map-value"} {
		if !flips[shape] {
			t.Fatalf("%s: the engine never refuses across the walk; widen it", shape)
		}
	}

	t.Run("jdk", func(t *testing.T) {
		slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
		javac, java := javaTools(t)
		packet, err := javacodegen.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range packet {
			if _, ok := files[name]; !ok {
				files[name] = data
			}
		}
		files["Driver.java"] = []byte(textOracleDriver(cases))
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
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		out, err := exec.Command(java, "-ea", "-Xss512k", "-cp", dir, u.Package+".Driver").CombinedOutput()
		if err != nil {
			t.Fatalf("java: %v\n%s", err, out)
		}
		got := map[int]string{}
		for line := range strings.SplitSeq(string(out), "\n") {
			rest, ok := strings.CutPrefix(line, "CASE ")
			if !ok {
				continue
			}
			index, verdict, _ := strings.Cut(rest, " ")
			i, err := strconv.Atoi(index)
			if err != nil {
				t.Fatalf("driver line %q", line)
			}
			got[i] = verdict
		}
		for i, c := range cases {
			if got[i] != want[i] {
				t.Errorf("%s: java %q, engine %q", c.name, got[i], want[i])
			}
		}
	})
}

func depthCase(name string) (shape string, n int, ok bool) {
	at := strings.LastIndex(name, "-")
	if !strings.HasPrefix(name, "depth-") || at < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(name[at+1:])
	return name[:at], n, err == nil
}

func textOracleDriver(cases []textOracleCase) string {
	var calls strings.Builder
	for i, c := range cases {
		// a case that throws (a write assert, a stack overflow) is reported
		// by name and the rest still run
		fmt.Fprintf(&calls, "        try { %s(%d, %q); } catch (Throwable e) { System.out.println(\"CASE %d threw-\" + e.getClass().getSimpleName()); }\n", strings.ToLower(c.root), i, c.text, i)
	}
	return `package probe;

import java.nio.charset.StandardCharsets;

public final class Driver {
    static String hex(byte[] b) {
        StringBuilder s = new StringBuilder();
        for (byte x : b) { s.append(String.format("%02x", x & 0xff)); }
        return s.toString();
    }

    static void probe(int i, String text) {
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = ProbeVariable.fromJson(text.getBytes(StandardCharsets.UTF_8), r);
        if (v == null || r.refused || r.damaged) { System.out.println("CASE " + i + " refuse"); return; }
        byte[] json = ProbeVariable.toJson(v);
        byte[] wire;
        try { wire = ProbeVariable.save(v); } catch (AssertionError e) { wire = "save-threw".getBytes(StandardCharsets.UTF_8); }
        System.out.println("CASE " + i + " accept " + (json == null ? "toJson-refused" : hex(json)) + " " + (wire == null ? "save-refused" : hex(wire)));
    }

    static void flat(int i, String text) {
        TableVariable.Report r = new TableVariable.Report();
        FlatVariable.Value v = FlatVariable.fromJson(text.getBytes(StandardCharsets.UTF_8), r);
        if (v == null || r.refused || r.damaged) { System.out.println("CASE " + i + " refuse"); return; }
        byte[] json = FlatVariable.toJson(v);
        System.out.println("CASE " + i + " accept " + (json == null ? "toJson-refused" : hex(json)));
    }

    public static void main(String[] args) {
` + calls.String() + `    }
}
`
}
