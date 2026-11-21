package elixirtable

// THE BATCH TEST'S EXPECTED-VALUE BUILDER OVER EVERY ARRAY ELEMENT KIND a fixed
// table's message form carries (docs/SPEC-TABLES.md §3.3). The manifest's
// message rows hold arrays of tables and of unions only, so the builder
// (expectedArrayValue, messageform_batch_test.go) refused an array of integers
// and an array of enums before the native decoder ran, which blocked adding a
// message row with either lawful shape. Each row here is a schema and a value
// of its own: the Go oracle (internal/tablewire) writes the announcement and
// the batch, the builder renders the value LoadMessages must return, and the
// generated Elixir loads, saves and measures the batch. The table and union
// rows are the controls: shapes the builder already carried.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestMessageFormBatchElixirArrayShapes(t *testing.T) {
	t.Parallel()
	const head = "package tblarrayshapes\n\nenum Grade\n{\n    Bronze\n    Silver\n}\n\ntype Point\n{\n    x uint16\n}\n\nunion Shape\n{\n    point Point\n}\n\n"
	rows := []struct {
		name, fields, text string
	}{
		// The controls: element kinds the builder carried before this test.
		{"table", "    seq int32\n    values [2]Point\n", `{"seq": 7, "values": [{"x": 1}, {"x": 2}]}`},
		{"union", "    seq int32\n    values [2]Shape\n", `{"seq": 7, "values": [{"point": {"x": 1}}, {}]}`},
		// The reviewer's two shapes (F-124), then the rest of the class.
		{"integer", "    seq int32\n    values [2]uint16\n", `{"seq": 7, "values": [1, 2]}`},
		{"enum", "    seq int32\n    values [2]Grade\n", `{"seq": 7, "values": ["Bronze", "Silver"]}`},
		{"integer_one_at_zero", "    seq int32\n    values [2]uint16\n", `{"seq": 7, "values": [0, 2]}`},
		{"enum_one_at_none", "    seq int32\n    values [2]Grade\n", `{"seq": 7, "values": ["None", "Silver"]}`},
		{"integer_all_at_zero", "    seq int32\n    values [2]uint16\n", `{"seq": 7, "values": [0, 0]}`},
		{"enum_all_at_none", "    seq int32\n    values [2]Grade\n", `{"seq": 7, "values": ["None", "None"]}`},
		{"signed", "    seq int32\n    values [3]int16\n", `{"seq": 7, "values": [-1, 0, 32767]}`},
		{"bool", "    seq int32\n    values [3]bool\n", `{"seq": 7, "values": [true, false, true]}`},
		{"counted_integer", "    seq int32\n    values [..3]uint16\n", `{"seq": 7, "values": [5, 0]}`},
		{"counted_enum", "    seq int32\n    values [..3]Grade\n", `{"seq": 7, "values": ["Silver", "None"]}`},
		{"counted_empty", "    seq int32\n    values [..3]uint16\n", `{"seq": 7, "values": []}`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "ArrayShapes.schema"), []byte(head+"fixed table Root\n{\n"+row.fields+"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			jsonPath := filepath.Join(dir, "value.json")
			if err := os.WriteFile(jsonPath, []byte(row.text), 0o600); err != nil {
				t.Fatal(err)
			}
			u := messageLoadUnit(t, dir, []string{"ArrayShapes.schema"})
			st := u.Tables["Root"]
			if st == nil || !st.FixedDeclared {
				t.Fatal("the schema declares no fixed table Root")
			}
			m := tabletext.NewModel(u)
			inst := m.New(st)
			var report tabletext.Report
			if !m.Read(inst, []byte(row.text), &report) {
				t.Fatalf("the Go text reader refuses %s", row.text)
			}
			batch, err := tablewire.EncodeMessage(m, inst)
			if err != nil {
				t.Fatalf("the Go oracle refuses the value: %v", err)
			}
			ann, batchPath := filepath.Join(dir, "conn.bin"), filepath.Join(dir, "message.bin")
			if err := os.WriteFile(ann, tablewire.Announce(u), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(batchPath, batch, 0o600); err != nil {
				t.Fatal(err)
			}
			expected := expectedMessageBody(t, u, st, jsonPath)
			out, err := runMessageBatchProbe(t, u, ann, batchPath, roundTripBody(messageBatchRow{name: row.name, root: "Root"}, expected))
			if err != nil {
				t.Fatalf("expected %s\n%v\n%s", expected, err, out)
			}
		})
	}
}
