package elixirtable

import (
	"regexp"
	"strings"
	"testing"
)

// TestElixirVariableReadHasNoPlaceholderClauses holds the variable form's read
// to one rule (docs/SPEC-TABLES.md §4): bytes the reader cannot read refuse
// the whole load. No clause answers a placeholder for damaged text, for an
// element kind it does not name, or for a payload width it does not know.
func TestElixirVariableReadHasNoPlaceholderClauses(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

table Corpus | forms = "wire"
{
    names  [..4]string(16)
    values []int32
}
`)
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	const readSide = "# ---- the read side"
	const sharedPrimitives = "# ---- the shared primitives"
	src := ""
	read := ""
	for name, data := range out {
		s := string(data)
		at := strings.Index(s, readSide)
		if at < 0 {
			continue
		}
		end := strings.Index(s[at:], sharedPrimitives)
		if end < 0 {
			t.Fatalf("%s: the read side has no end marker", name)
		}
		src = s
		read = s[at : at+end]
		break
	}
	if src == "" {
		t.Fatal("generated no variable-form read side")
	}
	if strings.Contains(src, "defp utf8_prefix(") {
		t.Error("the generated unit keeps the JSON-only UTF-8 prefix helper without a caller")
	}

	if strings.Contains(read, "utf8_prefix") {
		t.Error("the variable-form read computes a truncated text prefix instead of refusing the load")
	}
	if regexp.MustCompile(`not utf8_valid\?\(raw\)\s*->\s*""`).MatchString(read) {
		t.Error("the variable-form read answers an empty string for ill-formed text instead of refusing the load")
	}

	plain := emittedClause(t, src, "defp read_list_elements(%{elem_kind: 12} = f,")
	if strings.Contains(plain, "utf8_prefix") || strings.Contains(plain, `-> ""`) {
		t.Error("the string element clause answers damaged or over-long text instead of refusing the load")
	}
	if !strings.Contains(plain, "throw(:wire_damaged)") {
		t.Error("the string element clause does not refuse damaged or over-long text")
	}

	unnamed := emittedClause(t, src, "defp read_list_elements(_f, _rest, _n, _nodes, _ids, acc, _report),")
	if !strings.Contains(unnamed, "throw(:wire_damaged)") {
		t.Error("the unnamed element kind clause answers the elements read so far instead of refusing the load")
	}

	fallback := emittedClause(t, src, "defp int_of(_payload, _w, _signed),")
	if strings.Contains(fallback, "do: 0") || !strings.Contains(fallback, "throw(:wire_damaged)") {
		t.Error("the int_of fallback answers a number for a payload width it does not know instead of refusing the load")
	}
}

// emittedClause returns the emitted text from head to the next clause at the
// same indentation, so a shape assertion names the clause and not a line.
func emittedClause(t *testing.T, src, head string) string {
	t.Helper()
	at := strings.Index(src, head)
	if at < 0 {
		t.Fatalf("generated read has no %q clause", head)
	}
	rest := src[at+len(head):]
	if end := strings.Index(rest, "\n  defp "); end >= 0 {
		return head + rest[:end]
	}
	return head + rest
}
