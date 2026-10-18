package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// A pack that refuses a text the read marked malformed carries that read's
// named Reason when it has one (docs/SPEC-TABLES.md §17.4): a timestamp that
// does not parse refuses as `timestamp_not_rfc3339: <field>` (§16.2), and the
// refusal names it instead of dropping it, wherever the text lives — a whole
// table, a field, an array element or an enum-keyed slot. A malformed text
// that names no rule keeps today's generic wording.
func TestPackErrorCarriesTheNamedReason(t *testing.T) {
	t.Parallel()

	const schema = `package probe

enum Kind
{
    A
    B
}

table Item
{
    at timestamp
}

table Root
{
    t string(8)
    at timestamp
}

table ListRoot
{
    items [..4]Item
}

table KeyedRoot
{
    items [Kind]Item
}

table GoalList | json = "array"
{
    goals []string(8)
}
`
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "probe.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(src, "probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("unit: %v", err)
	}

	// write puts one text in a fresh tree: a root file when name has no
	// separator, a nested file otherwise.
	write := func(t *testing.T, name, text string) string {
		t.Helper()
		tree := t.TempDir()
		path := filepath.Join(tree, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return tree
	}

	// A root that reads as one text of the whole table is refused by name with
	// the reason the read hands back.
	t.Run("root-carries-reason", func(t *testing.T) {
		tree := write(t, "Root.json", `{"at":"garbage"}`)
		_, _, _, err := c.Pack(u, "Root", tree)
		if err == nil {
			t.Fatal("the root was accepted")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// A field file that reads malformed with the same reason.
	t.Run("field-carries-reason", func(t *testing.T) {
		tree := write(t, "at.json", `"garbage"`)
		_, _, _, err := c.Pack(u, "Root", tree)
		if err == nil {
			t.Fatal("the field was accepted")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// An array element file that reads malformed with the same reason.
	t.Run("element-carries-reason", func(t *testing.T) {
		tree := write(t, "items/0.json", `{"at":"garbage"}`)
		_, _, _, err := c.Pack(u, "ListRoot", tree)
		if err == nil {
			t.Fatal("the element was accepted")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// An enum-keyed slot file that reads malformed with the same reason.
	t.Run("slot-carries-reason", func(t *testing.T) {
		tree := write(t, "items/A.json", `{"at":"garbage"}`)
		_, _, _, err := c.Pack(u, "KeyedRoot", tree)
		if err == nil {
			t.Fatal("the slot was accepted")
		}
		if !strings.Contains(err.Error(), "timestamp_not_rfc3339: at") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// An array-root table read as an object names the rule the read reports.
	t.Run("array-root-names-json-root-not-array", func(t *testing.T) {
		tree := write(t, "GoalList.json", `{"goals":["a"]}`)
		_, _, _, err := c.Pack(u, "GoalList", tree)
		if err == nil {
			t.Fatal("the object at an array root was accepted")
		}
		if !strings.Contains(err.Error(), "json_root_not_array: GoalList") {
			t.Fatalf("refusal drops the named reason:\n%s", err)
		}
	})

	// A malformed text that names no rule keeps today's generic wording.
	t.Run("bare-brace-keeps-wording", func(t *testing.T) {
		tree := write(t, "Root.json", `{`)
		_, _, _, err := c.Pack(u, "Root", tree)
		if err == nil {
			t.Fatal("the root was accepted")
		}
		if !strings.Contains(err.Error(), "not one JSON text for table Root") {
			t.Fatalf("generic wording changed:\n%s", err)
		}
	})
}
