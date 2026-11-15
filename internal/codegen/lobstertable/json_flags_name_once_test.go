package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// flagsNameOnceSchema is this card's probe unit: a flags field at the top
// level and a flags field inside a nested table.
const flagsNameOnceSchema = `package flagsonce

flags Caps { Jump, Crouch }

table Inner
{
    caps Caps
}

table Config
{
    caps  Caps
    inner Inner
}
`

// flagsNameOnceDoc is one JSON document and whether it names a known variant
// twice in one flags array.
type flagsNameOnceDoc struct {
	text  string
	twice bool
}

var flagsNameOnceDocs = []flagsNameOnceDoc{
	{`{"caps":["Jump","Jump"],"inner":{"caps":[]}}`, true},
	{`{"caps":[],"inner":{"caps":["Crouch","Crouch"]}}`, true},
	{`{"caps":["Jump"],"inner":{"caps":[]}}`, false},
	{`{"caps":[],"inner":{"caps":["Crouch"]}}`, false},
}

// flagsNameOnceUnit generates the probe unit into a fresh directory.
func flagsNameOnceUnit(t *testing.T) *loadUnit {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Config.schema")
	if err := os.WriteFile(schemaPath, []byte(flagsNameOnceSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the probe unit is refused: %v", err)
	}
	return g
}

// flagsProbeResult is one document's Lobster verdict and, when read, the
// canonical text it wrote back.
type flagsProbeResult struct {
	verdict string
	written []byte
}

// runFlagsNameOnceProbe reads every document through the generated runtime
// and reports each verdict.
func runFlagsNameOnceProbe(t *testing.T, g *loadUnit, docs []flagsNameOnceDoc) []flagsProbeResult {
	t.Helper()
	inputs := map[string][]byte{}
	for i, d := range docs {
		inputs[fmt.Sprintf("j%d", i)] = []byte(d.text)
		_ = os.Remove(filepath.Join(g.dir, fmt.Sprintf("w%d", i)))
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .strict_and_refusals\nimport .json\n")
	b.WriteString(`def flagsprobe(i:int):
    let rep = schema_rt.LoadReport {}
    let text = read_file("j{i}")
    assert text
    let rec = schema_rt.json_read("Config", text, rep)
    if rec:
        let out, why = schema_rt.json_write(rec)
        if why != "":
            print("E{i} write: " + why)
            return
        assert write_file("w{i}", out)
        print("R{i} read")
    else:
        print("R{i} refused")
`)
	for i := range docs {
		fmt.Fprintf(&b, "flagsprobe(%d)\n", i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	results := make([]flagsProbeResult, len(docs))
	seen := make([]bool, len(docs))
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		var i int
		var v string
		if _, err := fmt.Sscanf(line, "R%d %s", &i, &v); err == nil && strings.HasPrefix(line, "R") {
			results[i].verdict = v
			seen[i] = true
			continue
		}
		t.Fatalf("the flags probe printed %q", line)
	}
	for i := range docs {
		if !seen[i] {
			t.Fatalf("the flags probe printed no verdict for document %d", i)
		}
		if results[i].verdict == "read" {
			data, err := os.ReadFile(filepath.Join(g.dir, fmt.Sprintf("w%d", i)))
			if err != nil {
				t.Fatalf("document %d was read but wrote no text: %v", i, err)
			}
			results[i].written = data
		}
	}
	return results
}

// TestLobsterJsonFlagsNameOnce holds the generated JSON reader to the
// reference engine on a flags array: a known variant named twice is refused,
// and the same variant named once reads with its bit set (§16.2).
func TestLobsterJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	g := flagsNameOnceUnit(t)
	results := runFlagsNameOnceProbe(t, g, flagsNameOnceDocs)
	reads, refusals := 0, 0
	for i, d := range flagsNameOnceDocs {
		want, wantText := goJsonRead(t, g.u, "Config", []byte(d.text))
		if d.twice && want != "refused" {
			t.Fatalf("the reference reads %s, which names a known variant twice: the probe does not exercise the rule", d.text)
		}
		if !d.twice && want != "read" {
			t.Fatalf("the reference refuses %s, which names each variant once: the probe is not grounded", d.text)
		}
		if want == "read" {
			reads++
		} else {
			refusals++
		}
		got := results[i].verdict
		if got != want {
			t.Errorf("flags %s: lobster %s, the reference %s", d.text, got, want)
			continue
		}
		if got == "read" && string(results[i].written) != string(wantText) {
			t.Errorf("flags %s: accepted value is not the reference's at byte %s", d.text, divergence(string(results[i].written), string(wantText)))
		}
	}
	if reads == 0 || refusals == 0 {
		t.Fatalf("the probe compared %d reads and %d refusals: it is vacuous", reads, refusals)
	}
}

// TestLobsterJsonFlagsNameOnceNegativeControl removes the repeat test from an
// emitted copy of json.lobster and requires the probe to go red on exactly the
// documents that name a known variant twice.
func TestLobsterJsonFlagsNameOnceNegativeControl(t *testing.T) {
	t.Parallel()
	g := flagsNameOnceUnit(t)
	patchJson(t, g,
		`                if not found and v == name:
                    if (bits & shl(1, bit)) != 0:
                        return j_refuse(r, "duplicate_key")
                    bits = bits | shl(1, bit)`,
		`                if not found and v == name:
                    bits = bits | shl(1, bit)`)
	results := runFlagsNameOnceProbe(t, g, flagsNameOnceDocs)
	red, green := 0, 0
	for i, d := range flagsNameOnceDocs {
		want, wantText := goJsonRead(t, g.u, "Config", []byte(d.text))
		got := results[i].verdict
		diverged := got != want || (got == "read" && string(results[i].written) != string(wantText))
		switch {
		case d.twice && !diverged:
			t.Errorf("NEGATIVE CONTROL FAILED: without the repeat test %s still agrees with the reference", d.text)
		case d.twice:
			red++
		case diverged:
			t.Errorf("NEGATIVE CONTROL FAILED: without the repeat test the once-named document %s went red", d.text)
		default:
			green++
		}
	}
	if red == 0 || green == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: %d documents red and %d green: the control is vacuous", red, green)
	}
	t.Logf("without the repeat test the probe went red on exactly its %d repeated-name documents and agreed on its %d once-named documents", red, green)
}
