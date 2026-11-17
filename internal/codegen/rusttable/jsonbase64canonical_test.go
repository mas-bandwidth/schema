package rusttable

// THE CANONICAL BASE64 RULE ON THE RUST LEG (docs/SPEC-TABLES.md §16.2,
// §16.8; docs/rulings.json json-base64-canonical): a `bytes(N)` or `*bytes`
// JSON value is CANONICAL base64 of the string's decoded value — the standard
// alphabet, padded, pad bits zero — exactly the spelling the writer writes,
// and any other spelling that decodes refuses the read. The vectors are the
// standard's own testdata/wire/json-base64-canonical/ rows; the engine
// (internal/tabletext) is the oracle for the verdict and the bytes, and the
// Rust probe must agree on the verdict alone, and on the bytes for every
// accepted row. Two escape rows pin that the JSON string is decoded before
// base64 is judged (§16.2's "The JSON layer decides FIRST").
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestRustJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	vectorDir := filepath.Join(repo, "testdata/wire/json-base64-canonical")
	manifest, err := os.ReadFile(filepath.Join(vectorDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		name, schema, root, text string
		accepted                 bool
		pinned, data             []byte
	}
	var rows []*vector
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			t.Fatalf("vector row %q names fewer than five columns", line)
		}
		name, schema, root, file, verdict := fields[0], fields[1], fields[2], fields[3], fields[4]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("vector %s: unknown verdict %q", name, verdict)
		}
		body, err := os.ReadFile(filepath.Join(vectorDir, file))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, &vector{
			name: name, schema: schema, root: root, text: string(body),
			accepted: verdict == "read",
		})
	}
	if len(rows) == 0 {
		t.Fatal("the canonical-base64 vector directory holds no rows")
	}
	const streamSchema, streamRoot = "tables/stream/Stream.schema", "Chunk"
	rows = append(rows,
		&vector{name: "escaped-solidus-read", schema: streamSchema, root: streamRoot, text: `{"data":"\/\/\/\/"}`, accepted: true, pinned: []byte{0xff, 0xff, 0xff}},
		&vector{name: "escaped-letter-read", schema: streamSchema, root: streamRoot, text: `{"data":"\u0041Q=="}`, accepted: true, pinned: []byte{0x01}},
	)

	// STEP ONE: the engine is the reference. Every row must carry the verdict
	// the standard states, and an escape row the bytes the standard states.
	models := map[string]*tabletext.Model{}
	for _, r := range rows {
		model, ok := models[r.schema]
		if !ok {
			model = tabletext.NewModel(generateUnit(t, repo, []string{r.schema}))
			models[r.schema] = model
		}
		st := model.Lookup(r.root)
		if st == nil {
			t.Fatalf("vector %s: the unit declares no table %s", r.name, r.root)
		}
		value := model.New(st)
		var report tabletext.Report
		got := model.Read(value, []byte(r.text), &report)
		if got == report.Refused {
			t.Fatalf("vector %s: the engine answered ok=%t beside report %+v", r.name, got, report)
		}
		if got != r.accepted {
			t.Fatalf("vector %s: the engine reads %t, the standard says %t (%+v)", r.name, got, r.accepted, report)
		}
		if !got {
			continue
		}
		fv, found := value.FieldByKey("data")
		if !found {
			t.Fatalf("vector %s: the read value has no data field", r.name)
		}
		r.data = append([]byte(nil), fv.Cell.Str...)
		if r.pinned != nil && string(r.data) != string(r.pinned) {
			t.Fatalf("vector %s: the engine reads %x, the standard says %x", r.name, r.data, r.pinned)
		}
	}

	// A unit's rows ride one probe: the read root names its own entry point,
	// so a group may carry more than one root.
	groups := map[string][]*vector{}
	var order []string
	for _, r := range rows {
		if _, ok := groups[r.schema]; !ok {
			order = append(order, r.schema)
		}
		groups[r.schema] = append(groups[r.schema], r)
	}
	for _, schema := range order {
		u := generateUnit(t, repo, []string{schema})
		var body strings.Builder
		for _, r := range groups[schema] {
			fmt.Fprintf(&body, `{
    let text: &[u8] = &%s;
    let want: bool = %t;
    let want_bytes: &[u8] = &%s;
    let mut report = TableReport::default();
    let got = %s(text, &mut report);
    let accepted = matches!(&got, Some(_) if !report.damaged);
    if accepted != want {
        println!("MISMATCH %s: verdict={}, want={}; report={:?}", accepted, want, report);
    } else if accepted {
        if let Some(v) = &got {
            if v.data.as_slice() != want_bytes {
                println!("MISMATCH %s: bytes={:x?}, want={:x?}", v.data, want_bytes);
            }
        }
    }
}
`, rustByteVector([]byte(r.text)), r.accepted, rustByteVector(r.data), fn(r.root, "from_json_bytes"), r.name, r.name)
		}
		body.WriteString("println!(\"PROBE DONE\");\n")
		out, err := rustConstructProbe(t, u, body.String())
		if err != nil {
			t.Fatalf("%s: the generated reader did not run: %v\n%s", schema, err, out)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "MISMATCH ") {
				t.Errorf("%s", line)
			}
		}
		if !strings.Contains(out, "PROBE DONE") {
			t.Fatalf("%s: the probe did not finish:\n%s", schema, out)
		}
	}
}
