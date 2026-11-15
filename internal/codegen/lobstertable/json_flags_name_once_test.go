package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// flagsOnceSchema has a flags field at the top level and one inside a
// nested table, so both places a flags array is read are exercised.
const flagsOnceSchema = "package flagsonceprobe\n" +
	"flags Perm { Read, Write, Own }\n" +
	"table Inner { p Perm }\n" +
	"table Root { p Perm\n inner Inner }\n"

// flagsOnceDocs name one known variant once and twice, at the top level and
// inside the nested table; the verdict of each comes from the reference.
var flagsOnceDocs = []string{
	`{"p": ["Write"]}`,
	`{"p": ["Write", "Write"]}`,
	`{"p": ["Write", "Own", "Write"]}`,
	`{"inner": {"p": ["Own"]}}`,
	`{"inner": {"p": ["Own", "Own"]}}`,
	`{"p": ["Read"], "inner": {"p": ["Read", "Write", "Read"]}}`,
}

// flagsOnceAnchor is the runtime's test for a flags name already set.
const flagsOnceAnchor = "                    if (bits & shl(1, bit)) != 0:\n                        return j_refuse(r, \"malformed\")\n"

// flagsOnceRun drives json_read over every document and returns the verdict
// and, for a read, the written text, per document.
func flagsOnceRun(t *testing.T, g *loadUnit) ([]string, []string) {
	t.Helper()
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
def doc(i:int):
    let text = read_file("d{i}")
    assert text
    let report = schema_rt.LoadReport {}
    let value = schema_rt.json_read("Root", text, report)
    if value:
        let out, reason = schema_rt.json_write(value)
        assert reason == ""
        assert write_file("o{i}", out)
        print("D{i} read")
    else:
        print("D{i} refused")
`)
	inputs := map[string][]byte{}
	for i, d := range flagsOnceDocs {
		fmt.Fprintf(&program, "doc(%d)\n", i)
		inputs[fmt.Sprintf("d%d", i)] = []byte(d)
		_ = os.Remove(filepath.Join(g.dir, fmt.Sprintf("o%d", i)))
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	verdicts := make([]string, len(flagsOnceDocs))
	texts := make([]string, len(flagsOnceDocs))
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		var i int
		var v string
		if _, err := fmt.Sscanf(line, "D%d %s", &i, &v); err != nil || i < 0 || i >= len(flagsOnceDocs) {
			t.Fatalf("the program printed %q", line)
		}
		verdicts[i] = v
		if v == "read" {
			b, err := os.ReadFile(filepath.Join(g.dir, fmt.Sprintf("o%d", i)))
			if err != nil {
				t.Fatal(err)
			}
			texts[i] = string(b)
		}
	}
	return verdicts, texts
}

func flagsOnceUnit(t *testing.T) *loadUnit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "FlagsOnce.schema")
	if err := os.WriteFile(path, []byte(flagsOnceSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// flagsOnceDisagreements compares each document's Lobster verdict, and an
// accepted document's written value, with the reference engine's.
func flagsOnceDisagreements(t *testing.T, g *loadUnit, verdicts, texts []string) []string {
	t.Helper()
	var red []string
	read, refused := 0, 0
	for i, d := range flagsOnceDocs {
		want, wantText := goJsonRead(t, g.u, "Root", []byte(d))
		if want == "read" {
			read++
		} else {
			refused++
		}
		if verdicts[i] != want {
			red = append(red, fmt.Sprintf("%s: lobster %s, the reference %s", d, verdicts[i], want))
			continue
		}
		if want == "read" && strings.TrimSpace(texts[i]) != strings.TrimSpace(string(wantText)) {
			red = append(red, fmt.Sprintf("%s: the value read is not the reference's", d))
		}
	}
	if read == 0 || refused == 0 {
		t.Fatalf("the reference read %d and refused %d documents: the comparison is not grounded", read, refused)
	}
	return red
}

// TestLobsterJsonFlagsNameOnce holds the Lobster JSON reader to the
// reference on a flags array (docs/SPEC-TABLES.md §16.2): a known name twice
// in one array is a repeated key and refuses the read; named once, the bit
// is set.
func TestLobsterJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	g := flagsOnceUnit(t)
	verdicts, texts := flagsOnceRun(t, g)
	for _, r := range flagsOnceDisagreements(t, g, verdicts, texts) {
		t.Error(r)
	}
}

// TestLobsterJsonFlagsNameOnceNegativeControl removes the repeated-name test
// from the emitted runtime and requires the comparison to go red.
func TestLobsterJsonFlagsNameOnceNegativeControl(t *testing.T) {
	t.Parallel()
	g := flagsOnceUnit(t)
	patchJson(t, g, flagsOnceAnchor, "")
	verdicts, texts := flagsOnceRun(t, g)
	if red := flagsOnceDisagreements(t, g, verdicts, texts); len(red) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: without the repeated-name test the reader still agrees with the reference")
	}
}
