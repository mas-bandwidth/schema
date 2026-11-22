package rusttable

// A REQUIRED [Min..N]T keeps its presence at its birth count: the birth
// elision (internal/tablewire encodeArray, allBirth) holds for a field that is
// neither required nor optional, so a required array of
// Min elements at their zero is written, and the writer's own read of it holds
// the required field present. The committed fixture
// testdata/required-born/MANIFEST.tsv carries the schemas, the JSON values
// and the engine's canonical variable saves (required fresh children, a
// changed child, non-required fresh children, required fresh scalars); the
// engine reproduces every golden first, then the Rust save and read are held
// to it. A FUNCTIONAL TEST where it runs rustc: no clock, no socket, no
// network.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

type requiredBornCase struct {
	id, schema, json, golden string
	fresh                    bool
}

func requiredBornCases(t *testing.T) []requiredBornCase {
	t.Helper()
	dir := filepath.Join("testdata", "required-born")
	text, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []requiredBornCase
	for line := range strings.SplitSeq(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("MANIFEST.tsv: %q has %d columns, want 5", line, len(f))
		}
		cases = append(cases, requiredBornCase{
			id:     f[0],
			schema: filepath.Join(dir, f[1]),
			json:   filepath.Join(dir, f[2], "R.json"),
			golden: filepath.Join(dir, f[3]),
			fresh:  f[2] == "fresh",
		})
	}
	if len(cases) < 4 {
		t.Fatalf("MANIFEST.tsv names %d cases, want at least 4", len(cases))
	}
	return cases
}

func requiredBornRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRustRequiredBornArrayKeepsPresence(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	// one probe per schema, its cases in it
	bySchema := map[string][]requiredBornCase{}
	var order []string
	for _, c := range requiredBornCases(t) {
		if _, ok := bySchema[c.schema]; !ok {
			order = append(order, c.schema)
		}
		bySchema[c.schema] = append(bySchema[c.schema], c)
	}
	for _, schema := range order {
		u := unitFrom(t, string(requiredBornRead(t, schema)))
		m := tabletext.NewModel(u)
		var body strings.Builder
		var ids []string
		for _, c := range bySchema[schema] {
			text := string(bytes.TrimSpace(requiredBornRead(t, c.json)))
			golden := requiredBornRead(t, c.golden)
			// the oracle reproduces its own golden first
			if got := engineEncode(t, m, "R", text); !bytes.Equal(got, golden) {
				t.Fatalf("%s: the engine writes %x, the committed golden is %x", c.id, got, golden)
			}
			id := strings.ReplaceAll(c.id, "/", "_")
			fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); let v = r_from_json(%s, &mut rep);\n", rustRaw(text))
			fmt.Fprintf(&body, "      println!(\"REQ %s_text_save {}\", v.and_then(|v| r_save_checked(&v).ok()).map(|b| b == vec!%s).unwrap_or(false)); }\n", id, rustByteVector(golden))
			fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); let v = r_load(&vec!%s, &mut rep);\n", rustByteVector(golden))
			fmt.Fprintf(&body, "      println!(\"REQ %s_reads_golden {}\", v.is_some() && !rep.damaged); }\n", id)
			ids = append(ids, id+"_text_save", id+"_reads_golden")
			if c.fresh {
				fmt.Fprintf(&body, "    { let saved = r_save_checked(&R::default());\n")
				fmt.Fprintf(&body, "      println!(\"REQ %s_default_save {}\", saved.as_ref().map(|b| *b == vec!%s).unwrap_or(false));\n", id, rustByteVector(golden))
				fmt.Fprintf(&body, "      let mut rep = TableReport::default();\n")
				fmt.Fprintf(&body, "      println!(\"REQ %s_default_self_read {}\", saved.map(|b| r_load(&b, &mut rep).is_some()).unwrap_or(false)); }\n", id)
				ids = append(ids, id+"_default_save", id+"_default_self_read")
			}
		}
		out, err := rustConstructProbe(t, u, body.String())
		if err != nil {
			t.Fatalf("%s: the probe did not run: %v\n%s", schema, err, out)
		}
		for _, id := range ids {
			if !strings.Contains(out, "REQ "+id+" true") {
				t.Errorf("%s: the Rust variable form does not hold a required [Min..N]T present at its birth:\n%s", id, out)
			}
		}
	}
}
