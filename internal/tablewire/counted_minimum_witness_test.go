package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countedMinWitnessDir is the counted-minimum message corpus
// (docs/SPEC-TABLES.md §3.3(a), §22.2), read by every leg's test that runs it.
const countedMinWitnessDir = "../../testdata/conformance/feature-witnesses/counted-minimum-message"

// TestCountedMinimumMessageWitnessPins holds the corpus to the reference: each
// read row's verdict is the engine's own over the committed batch, and each
// write row is the engine's: a count outside [2, 4] is never written, and a
// count inside it writes the committed batch byte for byte (the birth value
// elided).
func TestCountedMinimumMessageWitnessPins(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(countedMinWitnessDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	text, err := os.ReadFile(filepath.Join(countedMinWitnessDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(countedMinWitnessDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	kinds := map[string]int{}
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv:%d: %d fields, want 5", i+1, len(f))
		}
		kind, name, value, verdict, want := f[0], f[1], f[2], f[3], f[4]
		kinds[kind+" "+verdict]++
		switch kind {
		case "read":
			v := &tablewire.Vocabulary{}
			var ar tabletext.Report
			if err := v.AnnounceRead(tablewire.Announce(u), &ar); err != nil {
				t.Fatalf("%s: announce: %v", name, err)
			}
			var r tabletext.Report
			n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(root)}, wire(value), v, &r)
			got := "refused"
			if err == nil && ok && n == 1 {
				got = "read"
			}
			if got != verdict {
				t.Errorf("%s: the engine's verdict on %s.bin is %s (%v), the row says %s", name, value, got, err, verdict)
			}
		case "write":
			count, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("%s: count %q", name, value)
			}
			in := m.New(root)
			if count < 2 || count > 4 {
				in.Fields[0].Count = count
			} else {
				var r tabletext.Report
				if !m.Read(in, []byte(`{"few":[0`+strings.Repeat(",0", count-1)+`]}`), &r) {
					t.Fatalf("%s: the engine refuses %d zeros", name, count)
				}
			}
			got, err := tablewire.EncodeMessages(m, []*tabletext.Instance{in})
			switch verdict {
			case "refused":
				if err == nil || want != "-" {
					t.Errorf("%s: the engine writes count %d (%x), the row says refused", name, count, got)
				}
			case "wrote":
				if err != nil {
					t.Errorf("%s: the engine refuses count %d: %v", name, count, err)
				} else if b := wire(want); !bytes.Equal(got, b) {
					t.Errorf("%s: %s.bin is %s, the engine writes %s", name, want, hex.EncodeToString(b), hex.EncodeToString(got))
				}
			default:
				t.Errorf("%s: verdict %q", name, verdict)
			}
		default:
			t.Errorf("%s: kind %q", name, kind)
		}
	}
	for _, k := range []string{"read read", "read refused", "write wrote", "write refused"} {
		if kinds[k] == 0 {
			t.Errorf("cases.tsv has no %q row: a corpus without its control holds nothing", k)
		}
	}
}
