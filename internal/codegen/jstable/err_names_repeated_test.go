package jstable

import (
	"strings"
	"testing"
)

// A REPEATED JSON KEY REFUSES THE READ, AND err() SAYS SO (docs/SPEC-TABLES.md
// §16.2; docs/plans/strict-only.md decision 5). The text read counts the
// repeat in report.repeated, and the strict mark refuses the whole read on it,
// handing back null. err() is the caller's one test for a refusal ("null when
// the strict rule accepts the read"), so a read refused on a repeated key alone
// must not answer null there: it names wire_damaged, as every other malformed
// text does in this leg. This is the fast half; the node probe in
// TestJavaScriptTextRepeatedKeyEveryObject runs the reads.
func TestJavaScriptErrNamesARepeatedKey(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Root {
 id int32
}
`)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	var src string
	for name, data := range files {
		if strings.HasSuffix(name, "Variable.js") {
			src = string(data)
		}
	}
	if src == "" {
		t.Fatalf("no <Base>Variable.js among %d generated files", len(files))
	}
	start := strings.Index(src, "\n  err() {\n")
	if start < 0 {
		t.Fatal("the report has no err()")
	}
	end := strings.Index(src[start:], "\n  }\n")
	if end < 0 {
		t.Fatal("err() does not close")
	}
	body := src[start : start+end]
	if !strings.Contains(body, "this.repeated > 0") {
		t.Errorf("err() answers null for a read refused on a repeated key alone; it must name it:\n%s", body)
	}
	mark := strings.Index(src, "function TableVariableStrictMark(report) {")
	if mark < 0 || !strings.Contains(src[mark:mark+200], "report.repeated") {
		t.Error("the strict mark no longer counts report.repeated, so this test no longer pins the refusal it names")
	}
}
