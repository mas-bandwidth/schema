package elixirtable

// THE ROADMAP'S WRITES ROW ON THE ELIXIR LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §6 and docs/SPEC-TABLES.md
// §3.4 fixed form). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS: the harness is
// reachable on a tree that never built the C++ reference corpus. A clause that
// needs the emitted code to RUN is named by the existing test that runs it.
//
// elixir/W1 [verify] "write slack is template zeros": a fixed writer lays a
// value into the RECORD IMAGE; the bytes it does not name (a union's default
// arm, SPEC-TABLES §3.4 "the widest arm") are zero.
//
// elixir/W3 [owed] "zero behind a narrower arm": a union's arms are laid at the
// WIDEST arm's width, and the slack behind a narrower arm is zero-filled, never
// the neighbour's bytes.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapElixirT07Writes(t *testing.T) {
	t.Parallel()

	// A union whose arms are 8 and 4 bytes wide: the tag is one byte, the
	// widest arm is Alpha's int64, and Beta's int32 leaves four bytes behind.
	const widenUnion = `package probe

type Alpha { a int64 = 0 }
type Beta  { b int32 = 0 }

union Pick
{
    alpha Alpha
    beta  Beta
}

fixed table Root
{
    pick Pick
}
`

	tasks := []struct {
		id  string
		run func(t *testing.T)
	}{
		// SPEC-TABLES §3.4 "the widest arm": the arm no tag names (the
		// default/None arm) is the WIDEST width of template zeros. If the
		// writer stopped zeroing the unnamed arm this pad disappears.
		{"elixir/W1", func(t *testing.T) {
			text := t07UnionModule(t, widenUnion)
			if !strings.Contains(text, "0::size(8)-unit(8)") {
				t.Fatalf("the union's default arm is not zero-filled at the widest width:\n%s", text)
			}
		}},

		// §6 / SPEC-TABLES §3.4 "the slack behind a narrower arm zero-filled":
		// Beta is four bytes and Alpha is eight, so the narrower arm's writer
		// pads four bytes of template zeros behind itself.
		{"elixir/W3", func(t *testing.T) {
			text := t07UnionModule(t, widenUnion)
			if !strings.Contains(text, "0::size(4)-unit(8)") {
				t.Fatalf("a narrower arm leaves the bytes behind it unzeroed:\n%s", text)
			}
		}},
	}

	for _, tc := range tasks {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07UnionModule is the emitted fixed module of a unit and, with it, the writer
// of the union the schema declares.
func t07UnionModule(t *testing.T, src string) string {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	text := string(out["ProbeFixed"+".ex"])
	if text == "" {
		text = findGenerated(t, out, "fixed_write_into")
	}
	if !strings.Contains(text, "_fixed_write_into") {
		t.Fatalf("no union writer was emitted:\n%s", text)
	}
	return text
}
