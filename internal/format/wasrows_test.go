package format

import "testing"

// The second `was` row through schemafmt: a qualified variant ends its line,
// a payload-free arm's section follows its name, and both come back as they
// went (SPEC §7.4).
func TestFormatsVariantAndArmWas(t *testing.T) {
	src := "package probe\n\nenum Grade\n{\n    Bronze,\n    Argent|was=\"Silver\"\n    Gold\n}\n\ntype Ward\n{\n    charge float32\n}\n\nunion Effect\n{\n    shield Ward|was=\"ward\"\n    pong|was=\"ping\"\n}\n\ntable Cfg\n{\n    grade Grade\n    effect Effect\n}\n"
	out, err := Format("Probe.schema", []byte(src))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	for _, want := range []string{"    Argent | was = \"Silver\"\n    Gold\n", "shield Ward | was = \"ward\"\n", "pong        | was = \"ping\"\n"} {
		if !contains(string(out), want) {
			t.Errorf("formatted output lacks %q:\n%s", want, out)
		}
	}
	again, err := Format("Probe.schema", out)
	if err != nil {
		t.Fatalf("format twice: %v", err)
	}
	if string(again) != string(out) {
		t.Errorf("not idempotent:\n%s", again)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
