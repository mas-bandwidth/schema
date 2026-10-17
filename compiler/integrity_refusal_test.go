// The integrity check is carried by Go first (docs/SPEC-TABLES.md §2.14):
// every other target refuses a unit that declares it BY NAME, centrally, in
// Generate, and the checker refuses the spellings §2.14 names.
package compiler

import (
	"strings"
	"testing"
)

func TestIntegrityRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	u := unitFromSource(t, "package probe\ntable Plain\n{\n    x int32\n}\ntable Ledger | integrity = \"crc32c\"\n{\n    x int32\n}\n")
	for _, lang := range c.Targets() {
		_, err := c.Generate(u, lang, nil)
		if lang == "go" {
			if err != nil {
				t.Errorf("go refused: %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("target %s accepted the construct", lang)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "integrity on table Ledger") || !strings.Contains(msg, "target \""+lang+"\"") || !strings.Contains(msg, "§2.14") {
			t.Errorf("target %s refusal does not name the construct, the table and the target: %s", lang, msg)
		}
	}
	// A FIXED TABLE takes the key (§2.14, §3.4: once per form-3 file): Go
	// generates it and every other target refuses it by the same central name.
	fixed := unitFromSource(t, "package probe\nfixed table Reading | integrity = \"crc32c\"\n{\n    x int32\n}\n")
	for _, lang := range c.Targets() {
		_, err := c.Generate(fixed, lang, nil)
		if lang == "go" {
			if err != nil {
				t.Errorf("go refused the fixed table: %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "integrity on table Reading") || !strings.Contains(err.Error(), "§2.14") {
			t.Errorf("target %s on the fixed table: %v", lang, err)
		}
	}
	for _, tc := range []struct{ name, line, want string }{
		{"unknown value", `table T | integrity = "crc32"`, `integrity = "crc32" names no check`},
		{"empty value", `table T | integrity = ""`, "integrity takes one quoted check name"},
		{"non-string value", `table T | integrity = 32`, "integrity takes one quoted check name"},
		{"bare key", `table T | integrity`, "integrity"},
	} {
		errs := checkErrors(t, "package probe\n"+tc.line+"\n{\n    x int32\n}\n")
		if len(errs) == 0 {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		if !strings.Contains(errs[0].Error(), tc.want) {
			t.Errorf("%s: %v, want it to name %q", tc.name, errs[0], tc.want)
		}
	}
}
