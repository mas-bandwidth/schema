package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestRustAnswersFeatureWitnesses reads the feature-witness unit's two
// committed instances (testdata/conformance/feature-witnesses/full, the
// manifest's feature_full and feature_array_root) through the generated Rust
// read, and holds each to the Go engine: the read reads, its §16 text is the
// engine's text, and its write-back is the engine's bytes.
func TestRustAnswersFeatureWitnesses(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	fixture, err := filepath.Abs(filepath.Join("../../..", "testdata/conformance/feature-witnesses/full"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(fixture, "Features.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	cases := []struct{ root, file string }{
		{"Root", "Root.bin"},
		{"ArrayRoot", "ArrayRoot.bin"},
	}
	var body strings.Builder
	for _, tc := range cases {
		path := filepath.Join(fixture, tc.file)
		wire, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(tc.root))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil {
			t.Fatalf("%s: the Go engine refuses %s: %v %+v", tc.root, tc.file, err, report)
		}
		text, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: the Go engine writes no text: %v", tc.root, err)
		}
		again, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the Go engine writes no wire: %v", tc.root, err)
		}
		fmt.Fprintf(&body, `{
let wire=std::fs::read(%q).unwrap();
let mut report=TableReport::default();
let value=%s(&wire,&mut report);
assert!(value.is_some() && !report.refused && !report.damaged,"%s: the Rust read refused: {:?}",report);
let value=value.unwrap();
assert_eq!(%s(&value).expect("%s: the Rust text writer refused").trim_end(),%q,"%s: §16 text");
assert_eq!(%s(&value).expect("%s: the Rust save refused"),vec!%s,"%s: write-back bytes");
println!("%s ok");
}
`, path, fn(tc.root, "load"), tc.root,
			fn(tc.root, "to_json_checked"), tc.root, string(text), tc.root,
			fn(tc.root, "save_checked"), tc.root, rustByteVector(again), tc.root,
			tc.root)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("feature witnesses on the Rust read: %v\n%s", err, out)
	}
	for _, tc := range cases {
		if !strings.Contains(out, tc.root+" ok\n") {
			t.Fatalf("%s was not answered: %s", tc.root, out)
		}
	}
}
