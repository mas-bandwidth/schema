package testreg

import "testing"

// The owed record held a language's instance of a test out of the ready
// query and reported it MISSING without running it. It is retired under the
// no-skips ruling (docs/rulings.json): Load refuses the line, so the list
// cannot come back.
func TestLoadRefusesTheRetiredOwedRecord(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"roundtrip.fam.one","lang":"go","reason":"go answers it absent today"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "the owed record is retired")
}
