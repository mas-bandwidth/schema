package tabletext

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
)

// jsonLayerAccepts is the engine's JSON LAYER over one whole text
// (docs/SPEC-TABLES.md §16.12): the leading byte-order mark, one value, then
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

// engineSuite loads the suite for this engine. PENDING names the files a
// generated walk has not met yet; the engine has met every one, so it is held
// to the policy whole, and a PENDING line it agrees with is not its gap.
func engineSuite(t *testing.T) *jsontestsuite.Suite {
	t.Helper()
	s, err := jsontestsuite.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Pending = map[string]string{}
	return s
}

// TestJSONTestSuiteEngine holds the engine's JSON layer to JSONTestSuite
// (docs/SPEC-TABLES.md §16.12): every vendored file, against the table the
// standard carries.
func TestJSONTestSuiteEngine(t *testing.T) {
	t.Parallel()
	s := engineSuite(t)
	if n := len(s.Files); n != 318 {
		t.Fatalf("the corpus holds %d files, want 318", n)
	}
	mismatches, pending := jsontestsuite.Check(s, engineVerdicts(s))
	for _, p := range pending {
		t.Errorf("pending: %s", p)
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
	s := engineSuite(t)
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

	// a PENDING line excuses its file's disagreement, and dropping it names
	// that file
	rows = cloneRows(s.Rows)
	rows["y_array_empty.json"] = jsontestsuite.Row{File: "y_array_empty.json", Accept: false, Reason: "witness"}
	excused, err := jsontestsuite.Resolve(s.Files, rows, map[string]string{"y_array_empty.json": "witness-card"})
	if err != nil {
		t.Fatal(err)
	}
	if m, p := jsontestsuite.Check(excused, got); len(m) != 0 || len(p) != 1 || !strings.HasPrefix(p[0], "y_array_empty.json: ") {
		t.Fatalf("a pending disagreement: Check named %q and pending %q, want only y_array_empty.json pending", m, p)
	}
	short, err := jsontestsuite.Resolve(s.Files, rows, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := jsontestsuite.Check(short, got); len(m) != 1 || !strings.HasPrefix(m[0], "y_array_empty.json: ") {
		t.Fatalf("dropping the PENDING line: Check named %q, want exactly y_array_empty.json", m)
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
	maps.Copy(c, rows)
	return c
}
