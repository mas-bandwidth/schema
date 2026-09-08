package ctable

import (
	"strings"
	"testing"
)

func TestRetainedCodeAnchors(t *testing.T) {
	names := map[string]string{"load_body": "load_body_retain", "table_writer_id": "table_retain_writer_id"}
	source := `/* load_body(r,v) */
static int load_body (TableReader * r, Value * v) { table_writer_id (w, nested(1,2)); return 1; }
const char * text="load_body(r,v)";
`
	got := retainedCode(source, names, "load_body", "table_writer_id")
	for _, want := range []string{`/* load_body(r,v) */`, `"load_body(r,v)"`, `load_body_retain (TableReader * r, Value * v, TableRetainWalk retention)`, `table_retain_writer_id (w, nested(1,2), retention)`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	t.Run("missing", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("a missing required anchor was silent")
			}
		}()
		retainedCode(strings.ReplaceAll(source, "load_body (", "load_body_renamed ("), names, "load_body")
	})
}
