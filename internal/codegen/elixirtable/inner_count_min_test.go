package elixirtable

import (
	"strings"
	"testing"
)

// A COUNT MINIMUM IS HELD ONE RECORD DOWN, WHATEVER THE PARENT DECLARES
// (docs/SPEC.md §5.1 row 3; test/tables/inner and its inner-*-count-below-min
// rows in internal/ci/hostilecorpus/corpus/manifest.tsv). A child declares
// `vals [2..4]uint8`; each parent is an array of that child and nothing else,
// once with no minimum of its own (Loose) and once with a minimum of one
// (Tight), on a fixed root and on a variable root. A read that walks the
// parent reaches the child's minimum in both: the fixed decode holds the
// child's count to [2, 4] and every parent walks the child's own decode, and
// the variable read's descriptor of the child's field carries the minimum its
// count check refuses under.
//
// This is a unit test. It reads the generated source. It does not run Elixir,
// take real time, open a socket, or use the network. The hostile runner
// (make schema-elixir-runs-hostile-corpus) runs the same shape's rows.
func TestElixirInnerCountMinHeldOneRecordDown(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package inner

fixed table CountChild
{
    vals [2..4]uint8
}

fixed table FixedLooseCount { nest [..4]CountChild }
fixed table FixedTightCount { nest [1..4]CountChild }

table VarLooseCount { nest [..4]CountChild }
table VarTightCount { nest [1..4]CountChild }
`)
	files := generateElixir(t, u)

	var fixed string
	for name, data := range files {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") && strings.Contains(string(data), "def count_child_fixed_decode(\n") {
			fixed = string(data)
		}
	}
	if fixed == "" {
		t.Fatal("no generated fixed module decodes CountChild; this test would prove nothing")
	}

	// the child's own decode holds its count to its declared [2..4]
	child := elixirDefBody(t, fixed, "def count_child_fixed_decode(\n")
	if !strings.Contains(child, "R.clamps(c, n_vals, 2, 4)") {
		t.Errorf("CountChild's fixed decode does not hold vals to [2, 4]:\n%s", child)
	}

	// each parent holds its own count to its own bounds and walks the child's
	// decode, whatever minimum the parent itself declares
	for _, p := range []struct{ fn, own string }{
		{"def fixed_loose_count_fixed_decode(\n", "R.clamps(c, n_nest, 0, 4)"},
		{"def fixed_tight_count_fixed_decode(\n", "R.clamps(c, n_nest, 1, 4)"},
	} {
		body := elixirDefBody(t, fixed, p.fn)
		if !strings.Contains(body, p.own) {
			t.Errorf("%s does not hold its own count with %s:\n%s", p.fn, p.own, body)
		}
		if !strings.Contains(body, "count_child_fixed_list(") {
			t.Errorf("%s does not walk its children through CountChild's decode:\n%s", p.fn, body)
		}
	}

	table := string(files["Table.ex"])
	if table == "" {
		t.Fatal("no Table.ex; the variable form was not emitted")
	}
	// the variable read's descriptor of CountChild.vals carries the minimum
	at := strings.Index(table, `name: "CountChild"`)
	if at < 0 {
		t.Fatal("Table.ex describes no CountChild")
	}
	desc := table[at:]
	if end := strings.Index(desc, "\n    },\n"); end >= 0 {
		desc = desc[:end]
	}
	if !strings.Contains(desc, "min_count: 2") {
		t.Errorf("CountChild.vals's variable descriptor carries no min_count: 2:\n%s", desc)
	}
	// and the count check refuses a count under it before any element is read
	if !strings.Contains(table, "defp clamp_count(n, %{min_count: min}, _report) when n < min") {
		t.Error("the variable read's count check has no clause refusing a count under its minimum")
	}
}

// A RANGE IS HELD ONE RECORD DOWN ON THE MESSAGE READ, WHATEVER THE PARENT
// DECLARES (docs/SPEC-TABLES.md §3.3, §4; test/tables/inner and its
// inner-message-*-range-past-max rows). `min = 1, max = 9` rides in four
// announced bits, which spell 1..16, so the announced shape alone admits a 10:
// the read holds the value to the record's own declared range. Each record's
// field entry carries that range and read_body holds every value it reads to
// it, so a child under a parent with or without a minimum of its own is held.
//
// This is a unit test. It reads the generated source. It does not run Elixir,
// take real time, open a socket, or use the network. The hostile runner
// (make schema-elixir-runs-hostile-corpus) runs the same shape's rows.
func TestElixirInnerMessageRangeHeldOneRecordDown(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package inner

fixed table RangeChild
{
    level int32 = 1 | min = 1, max = 9
}

fixed table FixedLooseRange { nest [..4]RangeChild }
fixed table FixedTightRange { nest [1..4]RangeChild }
`)
	files := generateElixir(t, u)
	src := string(files[TableMessageModule+".ex"])
	if src == "" {
		t.Fatalf("the unit grows no %s.ex; this test would prove nothing", TableMessageModule)
	}

	// the child's record carries its declared range on its one field
	at := strings.Index(src, `"RangeChild" => %{`)
	if at < 0 {
		t.Fatal("the message records describe no RangeChild")
	}
	rec := src[at:]
	if end := strings.Index(rec, "}}"); end >= 0 {
		rec = rec[:end+2]
	}
	if !strings.HasSuffix(rec, ", {1, 9}}") {
		t.Errorf("RangeChild's message record does not carry level's range {1, 9}: %s", rec)
	}

	// and the read holds every value it reads, at every depth, to that range
	body := elixirDefBody(t, src, "defp read_body(vocab, r, acc, seen, depth, rec, last) do")
	if !strings.Contains(body, "true <- held?(rec, entry.id, value)") {
		t.Errorf("read_body does not hold a read value to its record's declared range:\n%s", body)
	}
	held := elixirDefBody(t, src, "defp held?(rec, id, value) do")
	if !strings.Contains(held, "value >= lo and value <= hi") {
		t.Errorf("held?/3 does not compare a value with its declared range:\n%s", held)
	}
}

// elixirDefBody is the generated source from head to the next top-level
// clause of the module, the one function's text.
func elixirDefBody(t *testing.T, src, head string) string {
	t.Helper()
	at := strings.Index(src, head)
	if at < 0 {
		t.Fatalf("the generated source has no %q", head)
	}
	body := src[at:]
	if end := strings.Index(body[len(head):], "\n  def"); end >= 0 {
		body = body[:len(head)+end]
	}
	return body
}
