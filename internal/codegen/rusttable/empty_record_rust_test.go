package rusttable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Empty records still occupy one byte in the schema C ABI; a Rust zero-sized
// row cannot represent either an empty node or consecutive empty list slots.
func TestRustEmptyRecordNative(t *testing.T) {
	slowtest.Gate(t, "rustc (empty cooked rows and list slots)")
	keyedRequireRustc(t)
	u := unitFrom(t, "package probe\ntable Empty {}\ntable Containers {\n values []Empty\n child Empty\n pointer *Empty\n}\n")
	m := tabletext.NewModel(u)
	work := t.TempDir()
	var body strings.Builder
	body.WriteString("assert_eq!(std::mem::size_of::<EmptyRow>(),1);\n")
	for _, tc := range []struct{ root, text string }{
		{"Empty", "{}"}, {"Containers", `{"values":[{},{},{}],"child":{},"pointer":{}}`},
	} {
		value := m.New(u.Tables[tc.root])
		var report tabletext.Report
		if !m.Read(value, []byte(tc.text), &report) || report != (tabletext.Report{}) {
			t.Fatal(report)
		}
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, "{let value=%s_from_json(%q,&mut TableReport::default()).unwrap();\nassert_eq!(%s_save(&value),vec![%s]);\n", ir.RustSnake(tc.root), tc.text, ir.RustSnake(tc.root), rustBytes(wire))
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, value, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(work, fmt.Sprintf("%s-%t", tc.root, big))
			if err = os.WriteFile(path, cooked, 0600); err != nil {
				t.Fatal(err)
			}
			order := 1
			if big {
				order = 2
			}
			fmt.Fprintf(&body, "assert_eq!(%s_cook_write(&value,%d).unwrap(),std::fs::read(%q).unwrap());\n", ir.RustSnake(tc.root), order, path)
		}
		fmt.Fprintf(&body, "println!(\"%s ok\");}\n", tc.root)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("empty row native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != 2 {
		t.Fatal(out)
	}
}
