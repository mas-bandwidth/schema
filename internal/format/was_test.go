package format

import "testing"

// The two `was` rows through schemafmt: a string or bytes default is its
// literal, a flags default is its brace list, and a table's qualification
// section reorders as a type's does (SPEC §7.4).
func TestFormatsValueDefaultsAndTableWas(t *testing.T) {
	src := "package probe\n\nflags Caps { Jump, Crouch }\n\ntable Ship | was = \"Vessel\", pinned\n{\n    name string(32) = \"untitled\"\n    tag bytes(4) = \"ab\"\n    caps Caps = {Jump,Crouch}\n    hull int32 = 100 | min = 0, max = 1000\n}\n"
	want := "package probe\n\nflags Caps { Jump, Crouch }\n\ntable Ship | pinned, was = \"Vessel\"\n{\n    name string(32) = \"untitled\"\n    tag  bytes(4) = \"ab\"\n    caps Caps = { Jump, Crouch }\n    hull int32 = 100             | min = 0, max = 1000\n}\n"
	out, err := Format("Probe.schema", []byte(src))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if string(out) != want {
		t.Errorf("formatted to:\n%s\nwant:\n%s", out, want)
	}
	again, err := Format("Probe.schema", out)
	if err != nil {
		t.Fatalf("format twice: %v", err)
	}
	if string(again) != string(out) {
		t.Errorf("not idempotent:\n%s", again)
	}
}
