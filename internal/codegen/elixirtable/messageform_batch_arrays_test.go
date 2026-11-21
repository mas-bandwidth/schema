package elixirtable

// THE BATCH TEST'S EXPECTED-VALUE BUILDER OVER EVERY ARRAY ELEMENT KIND a fixed
// table's message form carries (docs/SPEC-TABLES.md §3.3). The manifest's
// message rows hold arrays of tables and of unions only, so the builder
// (expectedArrayValue, messageform_batch_test.go) refused an array of integers
// and an array of enums before the native decoder ran, which blocked adding a
// message row with either lawful shape (review finding F-124 on #4450).
//
// The cases are committed corpus, testdata/messagearrayshapes: one schema with
// a fixed root per element kind, cases.tsv naming each case's root and value,
// and the announcement and one-body batch the Go oracle (internal/tablewire)
// writes for each, pinned beside them. The oracle half runs in every build and
// holds the pinned bytes to what the oracle writes today; the native half,
// under SCHEMA_SLOW, has the generated Elixir load, save and measure each
// pinned batch against the value the builder renders. The table and union
// cases are the controls: shapes the builder already carried.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// arrayShapesCase is one line of testdata/messagearrayshapes/cases.tsv.
type arrayShapesCase struct {
	name, root, text string
}

func arrayShapesCases(t *testing.T, dir string) []arrayShapesCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []arrayShapesCase
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("cases.tsv: %q is not name, root and value", line)
		}
		cases = append(cases, arrayShapesCase{f[0], f[1], f[2]})
	}
	// The two controls and the reviewer's two shapes are the floor.
	have := map[string]bool{}
	for _, c := range cases {
		have[c.name] = true
	}
	for _, want := range []string{"table", "union", "integer", "enum"} {
		if !have[want] {
			t.Fatalf("cases.tsv lost the %s case", want)
		}
	}
	return cases
}

// pinnedOracleBytes holds a committed corpus file to what the Go oracle
// writes, or rewrites it under SCHEMA_UPDATE_ARRAYSHAPES_CORPUS=1.
func pinnedOracleBytes(t *testing.T, path string, written []byte) {
	t.Helper()
	if os.Getenv("SCHEMA_UPDATE_ARRAYSHAPES_CORPUS") == "1" {
		if err := os.WriteFile(path, written, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	pinned, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the pinned corpus file %s is unreadable: %v", path, err)
	}
	if !bytes.Equal(pinned, written) {
		t.Fatalf("%s: the Go oracle writes % x, the corpus pins % x", path, written, pinned)
	}
}

func TestMessageFormBatchElixirArrayShapes(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs(filepath.Join("testdata", "messagearrayshapes"))
	if err != nil {
		t.Fatal(err)
	}
	cases := arrayShapesCases(t, dir)
	u := messageLoadUnit(t, dir, []string{"ArrayShapes.schema"})
	ann := filepath.Join(dir, "conn.bin")
	pinnedOracleBytes(t, ann, tablewire.Announce(u))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			st := u.Tables[c.root]
			if st == nil || !st.FixedDeclared {
				t.Fatalf("ArrayShapes.schema declares no fixed table %s", c.root)
			}
			m := tabletext.NewModel(u)
			inst := m.New(st)
			var report tabletext.Report
			if !m.Read(inst, []byte(c.text), &report) {
				t.Fatalf("the Go text reader refuses %s", c.text)
			}
			batch, err := tablewire.EncodeMessage(m, inst)
			if err != nil {
				t.Fatalf("the Go oracle refuses the value: %v", err)
			}
			batchPath := filepath.Join(dir, c.name+".message.bin")
			pinnedOracleBytes(t, batchPath, batch)
			jsonPath := filepath.Join(t.TempDir(), "value.json")
			if err := os.WriteFile(jsonPath, []byte(c.text), 0o600); err != nil {
				t.Fatal(err)
			}
			expected := expectedMessageBody(t, u, st, jsonPath)
			out, err := runMessageBatchProbe(t, u, ann, batchPath, roundTripBody(messageBatchRow{name: c.name, root: c.root}, expected))
			if err != nil {
				t.Fatalf("expected %s\n%v\n%s", expected, err, out)
			}
		})
	}
}
