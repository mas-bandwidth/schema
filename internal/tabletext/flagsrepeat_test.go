package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const flagsRepeatSchema = `package probe

flags Cap { Trace, Verbose }

table Root
{
    cap Cap
}
`

func flagsRepeatModel(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(flagsRepeatSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// TestJSONFlagsRepeatedNameRefused holds docs/SPEC-TABLES.md §16.2: a flags
// name twice in one array is a repeated key, and the read refuses malformed;
// two different names, an unknown name and an empty array read.
func TestJSONFlagsRepeatedNameRefused(t *testing.T) {
	t.Parallel()
	m := flagsRepeatModel(t)
	root := m.Lookup("Root")
	for _, c := range []struct {
		text   string
		accept bool
	}{
		{`{"cap":["Trace","Verbose"]}`, true},
		{`{"cap":["Trace","Nope"]}`, true},
		{`{"cap":[]}`, true},
		{`{"cap":["Trace","Trace"]}`, false},
		{`{"cap":["Verbose","Trace","Verbose"]}`, false},
	} {
		var r tabletext.Report
		got := m.Read(m.New(root), []byte(c.text), &r)
		if got != c.accept {
			t.Errorf("%s: accept=%v, want %v (%+v)", c.text, got, c.accept, r)
		}
		if !c.accept && !strings.HasPrefix(r.Reason, "malformed") {
			t.Errorf("%s: the refusal is %q, not malformed", c.text, r.Reason)
		}
	}
}

// TestJSONFlagsRepeatedNameNegativeControl removes the rule and requires the
// test red on the repeated names.
func TestJSONFlagsRepeatedNameNegativeControl(t *testing.T) {
	t.Parallel()
	out, failed := slowtest.RunSabotaged(t, "../..", "./internal/tabletext", "^TestJSONFlagsRepeatedNameRefused$",
		slowtest.Sabotage{File: "internal/tabletext/read.go",
			From: "if bits&(uint64(1)<<uint(bit)) != 0 {", To: "if false && bits&(uint64(1)<<uint(bit)) != 0 {"})
	if !failed || !strings.Contains(out, `{"cap":["Trace","Trace"]}: accept=true`) {
		t.Errorf("negative control: the rule is gone and the test did not go red on its own words\n%s", out)
	}
}
