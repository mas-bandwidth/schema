package tabletext

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
)

// jsonLayerAccepts is the engine's JSON LAYER over one whole text
// (docs/SPEC-TABLES.md §16.11): the leading byte-order mark, one value, then
// whitespace, then the end of the input, under the reader's three caps. It
// drives the reader the table walk drives, in-package, with no new API.
func jsonLayerAccepts(text []byte) bool {
	var report Report
	in := &reader{text: text, report: &report}
	in.byteOrderMark()
	ok := in.skipValue(0)
	in.space()
	return ok && !in.bad && in.pos == len(in.text)
}

func engineVerdicts(s *jsontestsuite.Suite) map[string]bool {
	got := map[string]bool{}
	for name, data := range s.Files {
		got[name] = jsonLayerAccepts(data)
	}
	return got
}

// TestJSONTestSuiteEngine holds the engine's JSON layer to JSONTestSuite
// (docs/SPEC-TABLES.md §16.11): every vendored file, against the table the
// standard carries, with PENDING's files allowed to disagree and logged.
func TestJSONTestSuiteEngine(t *testing.T) {
	t.Parallel()
	s, err := jsontestsuite.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.Files); n != 318 {
		t.Fatalf("the corpus holds %d files, want 318", n)
	}
	mismatches, pending := jsontestsuite.Check(s, engineVerdicts(s))
	for _, p := range pending {
		t.Logf("pending: %s", p)
	}
	for _, m := range mismatches {
		t.Errorf("mismatch: %s", m)
	}
}

// TestJSONTestSuitePolicyWitness is the reversed witness for the harness: each
// edit to an in-memory copy of the loaded policy is named, by file, by the
// check that must catch it.
func TestJSONTestSuitePolicyWitness(t *testing.T) {
	t.Parallel()
	s, err := jsontestsuite.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	got := engineVerdicts(s)
	if m, _ := jsontestsuite.Check(s, got); len(m) != 0 {
		t.Fatalf("the unedited policy is not clean, so the witness proves nothing: %v", m)
	}

	// flip a y_ file and an i_ file to refuse: Check names exactly those two
	rows := cloneRows(s.Rows)
	for _, name := range []string{"y_array_empty.json", "i_number_huge_exp.json"} {
		rows[name] = jsontestsuite.Row{File: name, Accept: false, Reason: "witness"}
	}
	flipped, err := jsontestsuite.Resolve(s.Files, rows, s.Pending)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := jsontestsuite.Check(flipped, got)
	if len(m) != 2 || !strings.HasPrefix(m[0], "i_number_huge_exp.json: want refuse, got accept") || !strings.HasPrefix(m[1], "y_array_empty.json: want refuse, got accept") {
		t.Fatalf("flipping two rows: Check named %q, want exactly i_number_huge_exp.json and y_array_empty.json", m)
	}

	// delete an i_ row: the loader's error names the file
	rows = cloneRows(s.Rows)
	delete(rows, "i_structure_500_nested_arrays.json")
	if _, err := jsontestsuite.Resolve(s.Files, rows, s.Pending); err == nil || !strings.Contains(err.Error(), "i_structure_500_nested_arrays.json: an i_ file with no row") {
		t.Fatalf("deleting the i_structure_500_nested_arrays row: err %v, want one naming it", err)
	}

	// drop one PENDING line: Check names that file
	pending := map[string]string{}
	for k, v := range s.Pending {
		pending[k] = v
	}
	if len(pending) == 0 {
		t.Fatal("PENDING is empty, so the PENDING half of the witness proves nothing")
	}
	names := make([]string, 0, len(pending))
	for k := range pending {
		names = append(names, k)
	}
	slices.Sort(names)
	dropped := names[0]
	delete(pending, dropped)
	short, err := jsontestsuite.Resolve(s.Files, s.Rows, pending)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = jsontestsuite.Check(short, got)
	if len(m) != 1 || !strings.HasPrefix(m[0], dropped+": ") {
		t.Fatalf("dropping the PENDING line for %s: Check named %q, want exactly that file", dropped, m)
	}

	// and a PENDING line whose file now agrees is itself named
	agreeing := s.Clone()
	agreeing.Pending["y_array_empty.json"] = "witness-card"
	if m, _ := jsontestsuite.Check(agreeing, got); len(m) != 1 || !strings.Contains(m[0], "y_array_empty.json") || !strings.Contains(m[0], "met: remove it from PENDING") {
		t.Fatalf("a pending file that agrees: Check named %q, want y_array_empty.json met", m)
	}
}

func cloneRows(rows map[string]jsontestsuite.Row) map[string]jsontestsuite.Row {
	c := make(map[string]jsontestsuite.Row, len(rows))
	for k, v := range rows {
		c[k] = v
	}
	return c
}
