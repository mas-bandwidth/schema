package testreg

import "testing"

// TestLoadRefusesAnOwedRecord: there is no owed record (Glenn, 2026-11-25:
// no skips and no exemption list of any kind). A test a language fails is an
// ordinary red row, so a line that would excuse one is refused by name.
func TestLoadRefusesAnOwedRecord(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"roundtrip.fam.one","lang":"go","reason":"go answers it absent today"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "an owed record is an exemption list")
}
