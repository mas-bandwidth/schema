package elixirtable

import (
	"fmt"
	"strings"
	"testing"
)

// TestElixirFixedClassFormThree pins the fixed class on the table wire
// (docs/SPEC-TABLES.md §3, §3.4). A declared fixed root's load and save are
// form 3. The read is one read: the whole value (one record) or a refusal.
// Form 1 is previous_form and nothing is decoded. A variable root in the same
// unit stays on form 1.
//
// This is a unit test. It reads the generated source. It does not run Elixir,
// take real time, open a socket, or use the network.
func TestElixirFixedClassFormThree(t *testing.T) {
	t.Parallel()
	files := fixedClassFormThreeFiles(t)
	if err := fixedClassFormThreeHolds(files); err != nil {
		t.Fatal(err)
	}
}

// TestElixirFixedClassFormThreeNegativeControl plants the form-1 fault in the
// real generated Table.ex (the table-wire artifact) and requires the same
// rule to fail. A control that does not touch that artifact proves nothing.
func TestElixirFixedClassFormThreeNegativeControl(t *testing.T) {
	t.Parallel()
	files := fixedClassFormThreeFiles(t)
	if err := fixedClassFormThreeHolds(files); err != nil {
		t.Fatalf("the real artifact does not hold the rule, so the control has nothing to break: %v", err)
	}
	src := string(files["Table.ex"])
	const anchor = "point_fixed_save([value])"
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("negative control target %q appears %d times in generated Table.ex, want 1", anchor, strings.Count(src, anchor))
	}
	// The fault the fixed-class pin used to ship: this root's save writes form 1.
	files["Table.ex"] = []byte(strings.Replace(src, anchor, "<<1>>", 1))
	err := fixedClassFormThreeHolds(files)
	if err == nil {
		t.Fatal("negative control passed: Table.ex with a form-1 save still held the rule")
	}
	if !strings.Contains(err.Error(), "form 3") {
		t.Fatalf("negative control failed for a different reason: %v", err)
	}
}

func fixedClassFormThreeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	u := unitFrom(t, `package probe

fixed table Point
{
    x int32
    y int32
}

table Note
{
    label string(8)
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["Table.ex"]; !ok {
		t.Fatal("no Table.ex; the table wire was not emitted")
	}
	fixed := false
	for name, data := range files {
		if strings.HasSuffix(name, "Fixed.ex") && strings.Contains(string(data), "def point_fixed_load(") {
			fixed = true
		}
	}
	if !fixed {
		t.Fatal("the fixed form was not emitted for Point; this test would prove nothing")
	}
	return files
}

// fixedClassFormThreeHolds is the rule, over the generated files and over a
// copy of Table.ex with a fault planted in it.
func fixedClassFormThreeHolds(files map[string][]byte) error {
	src := string(files["Table.ex"])
	if !strings.Contains(src, "<<1>>") {
		return fmt.Errorf("the variable codec no longer writes form 1; this card moves only the fixed root")
	}
	if strings.Contains(src, `def save("Note"`) || strings.Contains(src, `def load("Note"`) {
		return fmt.Errorf("variable root Note was moved onto the fixed-class wire")
	}
	save, ok := elixirClause(src, `  def save("Point", value)`)
	if !ok {
		return fmt.Errorf("Point save is not form 3: no fixed-root save clause")
	}
	if !strings.Contains(save, "point_fixed_save([value])") {
		return fmt.Errorf("Point save is not form 3: it does not call point_fixed_save")
	}
	if strings.Contains(save, "<<1>>") || strings.Contains(save, "<<1,") {
		return fmt.Errorf("Point save is not form 3: the clause writes form 1")
	}
	refused, ok := elixirClause(src, `  def load("Point", <<1, _::binary>>)`)
	if !ok || !strings.Contains(refused, ":previous_form") {
		return fmt.Errorf("Point load does not refuse form 1 as previous_form")
	}
	if strings.Contains(refused, "point_fixed_load") || strings.Contains(refused, "open(") {
		return fmt.Errorf("form 1 at Point is decoded; previous_form must decode nothing")
	}
	load, ok := elixirClause(src, `  def load("Point", data)`)
	if !ok {
		return fmt.Errorf("Point load is not form 3: no fixed-root load clause")
	}
	if !strings.Contains(load, "point_fixed_load(data)") {
		return fmt.Errorf("Point load is not form 3: it does not call point_fixed_load")
	}
	if !strings.Contains(load, "{:ok, [value], report} -> {:ok, value, report}") {
		return fmt.Errorf("Point load does not return the one record, the whole value")
	}
	if strings.Contains(load, "hd(") || strings.Contains(load, "{:ok, values") {
		return fmt.Errorf("Point load can return part of the batch")
	}
	if !strings.Contains(load, "{:ok, [], report} -> {:error, :truncated, report}") {
		return fmt.Errorf("an empty fixed batch is not a refusal")
	}
	if !strings.Contains(load, "{:ok, _values, report} -> {:error, :batch_too_large, report}") {
		return fmt.Errorf("a multi-record fixed batch is not a refusal")
	}
	return nil
}

// elixirClause returns the function clause whose head starts at head, through
// the line before the next def at the same column.
func elixirClause(src, head string) (string, bool) {
	i := strings.Index(src, head)
	if i < 0 {
		return "", false
	}
	if i > 0 && src[i-1] != '\n' {
		return "", false
	}
	rest := src[i:]
	if j := strings.Index(rest[1:], "\n  def"); j >= 0 {
		return rest[:1+j], true
	}
	return rest, true
}
