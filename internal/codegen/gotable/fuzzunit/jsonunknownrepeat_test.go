package tabledemo_test

import (
	"os"
	"strings"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

// TestGeneratedJSONRepeatedDecodedKeysRefuse holds docs/SPEC-TABLES.md §16.2
// on the generated reader, as tabletext holds it: a key repeated once decoded
// refuses malformed whether the reader names it or not. That covers None twice
// and an unknown variant twice in a keyed object (the json-hostile rows
// keyed-duplicate-none and keyed-duplicate-unknown), and an unknown field key
// twice, spelled the same or through an escape. Distinct unknown keys read.
func TestGeneratedJSONRepeatedDecodedKeysRefuse(t *testing.T) {
	t.Parallel()
	pack := func(text string) (bool, tabledemo.TableReport) {
		var v tabledemo.PackConfig
		var r tabledemo.TableReport
		return tabledemo.PackConfigFromJSON(&v, []byte(text), &r), r
	}
	for _, row := range []string{"keyed-duplicate-none", "keyed-duplicate-unknown"} {
		text, err := os.ReadFile("../../../../testdata/conformance/tables/json-hostile/" + row + "/PackConfig.json")
		if err != nil {
			t.Fatal(err)
		}
		if ok, r := pack(string(text)); ok || r.Reason != "malformed" {
			t.Errorf("%s: accept=%v reason=%q, want a malformed refusal", row, ok, r.Reason)
		}
	}
	for _, c := range []struct {
		text   string
		accept bool
	}{
		{`{"ships":{"Future":{"name":"A"},"Future":{"name":"B"}}}`, false},
		{`{"ships":{"Future":{"name":"A"},"None":{"name":"B"}}}`, true},
		{`{"new":1,"new":2}`, false},
		{`{"new":1,"new":2}`, false},
		{`{"new":1,"other":2}`, true},
	} {
		ok, r := pack(c.text)
		if ok != c.accept || (!ok && !strings.HasPrefix(r.Reason, "malformed")) {
			t.Errorf("%s: accept=%v reason=%q, want accept=%v", c.text, ok, r.Reason, c.accept)
		}
	}
}
