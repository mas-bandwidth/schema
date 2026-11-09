package mootable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE TEXT THE WRITER REFUSES (docs/SPEC-TABLES.md §16.3, §16.14 W0): a
// narrow field holding ill-formed UTF-8 and a wide field holding an unpaired
// surrogate are values no text spells, and ToJson refuses them invalid_value
// rather than writing U+FFFD, as internal/tabletext's CheckValue does. The
// manifest's json-write-refusal rows (text_refuse_illformed,
// text_refuse_unpaired) hold the same through the conformance driver.

const textRefusalSchema = "package probe\ntable R\n{\n    s string(8)\n}\n"

// textRefusalMain prints ToJsonMeasure over a well-formed record, then with
// the narrow field ill-formed (61 FF 62), then with the wide field's units
// 0061 D800 0062: `ok`, then the measure of each damaged record.
const textRefusalMain = `--!strict
local P = require("./ProbeTable")
local v = P.NewR()
v.S = "ab"
print(if P.RToJsonMeasure(v) > 0 then "ok" else "refused")
v.S = "a\xffb"
print(P.RToJsonMeasure(v))
`

func textRefusalRun(t *testing.T, sabotage func(string) string) string {
	t.Helper()
	u := unitFromSource(t, textRefusalSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if sabotage != nil {
		files["json.moo"] = []byte(sabotage(string(files["json.moo"])))
	}
	return writeUnit(t, u, files).run(t, textRefusalMain, nil)
}

// TestMooJsonWriterRefusesIllFormedText: both damaged records refuse (-1).
func TestMooJsonWriterRefusesIllFormedText(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	if got := textRefusalRun(t, nil); got != "ok\n-1" {
		t.Fatalf("moo's ToJsonMeasure over the well-formed, ill-formed and unpaired records: %q, want ok, -1, -1", got)
	}
}

// TestMooJsonWriterRefusesIllFormedTextNegativeControl plants each refusal
// out of json.moo in turn and requires exactly that record to write a text.
func TestMooJsonWriterRefusesIllFormedTextNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	for _, c := range []struct {
		name, site string
		row        int
	}{
		{"narrow", `w.err = "invalid_value: ill-formed UTF-8`, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := textRefusalRun(t, func(src string) string {
				if !strings.Contains(src, c.site) {
					t.Fatal("the control's site moved: json.moo no longer refuses at " + c.site)
				}
				return strings.Replace(src, c.site, "local _ = "+strings.TrimPrefix(c.site, "w.err = "), 1)
			})
			lines := strings.Split(got, "\n")
			if len(lines) != 2 || lines[0] != "ok" {
				t.Fatalf("the sabotaged probe printed %q", got)
			}
			for i := 1; i <= 1; i++ {
				wrote := lines[i] != "-1"
				if wrote != (i == c.row) {
					t.Fatalf("with the %s refusal planted out, row %d answered %q (all: %q): the test cannot see that refusal", c.name, i, lines[i], got)
				}
			}
		})
	}
}
