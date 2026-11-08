package jstable

import (
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJSManifestWideLiteralChecksEveryCodeUnit(t *testing.T) {
	slowtest.Gate(t, "node (wide literal manifest assertion)")
	assertion, ok := jsManifestCompare("value", `u"A\uD83D\uDE00\uFFFF"`)
	if !ok {
		t.Fatal("wide manifest value was silently omitted")
	}
	for _, tc := range []struct {
		name, units string
		reject      bool
	}{
		{"exact", "65,0xd83d,0xde00,0xffff", false},
		{"wrong-high-surrogate", "65,0xd83e,0xde00,0xffff", true},
		{"wrong-low-surrogate", "65,0xd83d,0xde01,0xffff", true},
		{"wrong-final-unit", "65,0xd83d,0xde00,0xfffe", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := "const value=new Uint16Array([" + tc.units + "]);const fail=()=>{throw new Error('manifest mismatch')};" + assertion
			out, err := exec.Command(jsNode(t), "--input-type=module", "-e", script).CombinedOutput()
			if (err != nil) != tc.reject {
				t.Fatalf("reject=%t err=%v: %s", tc.reject, err, out)
			}
		})
	}
}
