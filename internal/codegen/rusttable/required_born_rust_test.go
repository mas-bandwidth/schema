package rusttable

// A REQUIRED [Min..N]T keeps its presence at its birth count: the birth
// elision (internal/tablewire encodeArray, allBirth; messageencode.go) holds
// for a field that is neither required nor optional, so a required array of
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
			fmt.Fprintf(&body, "      println!(\"REQ %s_text_save {}\", v.and_then(|v| r_save_checked(&v)).map(|b| b == vec!%s).unwrap_or(false)); }\n", id, rustByteVector(golden))
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

const requiredCountedMessageSrc = `package requiredcounted

type Inner
{
    a int32 = 3
}

fixed table Root
{
    ticks uint32
    ps    [2..3]Inner | required
}
`

func TestRustMessageRequiredCountedBornRides(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, requiredCountedMessageSrc)
	m := tabletext.NewModel(u)
	saves := []struct{ id, text, set string }{
		{"ps_born_required", `{"ticks":5,"ps":[{},{}]}`, "v.ps_count = 2; v.ps[0] = inner_message_default(); v.ps[1] = inner_message_default();"},
		{"ps_live_required", `{"ticks":5,"ps":[{"a":4},{}]}`, "v.ps_count = 2; v.ps[0] = inner_message_default(); v.ps[1] = inner_message_default(); v.ps[0].a = 4;"},
	}
	var head string
	var body strings.Builder
	for i, c := range saves {
		wire := repeatedIdEncode(t, m, "Root", c.text)
		name := fmt.Sprintf("S%d", i)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(wire), rustBytes(wire))
		fmt.Fprintf(&body, `
    {
        let mut v = root_message_default();
        v.ticks = 5;
        %s
        let values = vec![v; 1];
        let mut buf = vec![0u8; 64];
        let wrote = root_save_messages(&values, &mut buf, &mut TableReport::default());
        let got = &buf[..wrote.max(0) as usize];
        say(%q, got == &%s[..], format!("wrote={wrote} got={got:02x?} want={:02x?}", %s));
    }
`, c.set, "save_"+c.id, name, name)
	}
	// the engine writes the required birth: it is not the empty body
	if bytes.Equal(repeatedIdEncode(t, m, "Root", saves[0].text), repeatedIdEncode(t, m, "Root", `{"ticks":5}`)) {
		t.Fatal("the oracle elides a required counted array at its birth; the test expects it to ride")
	}
	out := runMessageBatchProbe(t, u, head+body.String())
	for _, c := range saves {
		id := "save_" + c.id
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: the Rust message form does not keep a required counted array at its birth: %s", id, line)
		}
	}
}
