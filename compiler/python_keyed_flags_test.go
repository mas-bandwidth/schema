package compiler

import (
	"strings"
	"testing"
)

func TestPythonKeyedFlagsPublicDriver(t *testing.T) {
	u := unitFromSource(t, `package keyflags
enum Slot { A, B }
flags Caps { A, B, C }
table Root { values [Slot]Caps }
`)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["keyflags/Probe.py"])
	start := strings.Index(src, `name="values",`)
	if start < 0 {
		t.Fatal("missing values descriptor")
	}
	row, _, _ := strings.Cut(src[start:], "\n        ),")
	for _, want := range []string{`codec="keyed",`, `elem=FieldDesc(`, `kind=9,`, `codec="int",`, `hi=18446744073709551615,`} {
		if !strings.Contains(row, want) {
			t.Errorf("descriptor lacks %s:\n%s", want, row)
		}
	}
	if strings.Contains(row, `"unsupported"`) {
		t.Errorf("unsupported descriptor: %s", row)
	}
}
