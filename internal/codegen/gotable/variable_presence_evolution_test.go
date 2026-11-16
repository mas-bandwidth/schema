package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"strings"
	"testing"
)

func TestGoVariablePresenceEvolution(t *testing.T) {
	t.Parallel()
	const prefix = `package probe
 enum Key { First, Second }
 table Child { value int32 }
 type Payload { value int32 }
 union Choice { number Payload }
 table Root { tick uint8
 `
	for _, tc := range []struct {
		name, fields, text string
		optional           bool
	}{
		{"required", `scalar int32
 text string(8) = "abc"
 blob bytes(8) = "abc"
 kid Child
 ptr *Child
 pick Choice
 values [2]int32
 counted [..2]int32
 links [2]*Child
 list []int32
 entries map[int32]int32
 keyed [Key]int32`, `{"tick":1,"scalar":0,"text":"abc","blob":"YWJj","kid":{},"ptr":null,"pick":{},"values":[0,0],"counted":[],"links":[null,null],"list":[],"entries":{},"keyed":{}}`, false},
		{"optional", `scalar int32
 kid Child
 values [2]int32
 counted [..2]int32
 entries map[int32]int32`, `{"tick":1,"scalar":0,"kid":{},"values":[0,0],"counted":[],"entries":{}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := strings.Split(tc.fields, "\n")
			var writerFields []string
			for _, line := range fields {
				line = strings.TrimSpace(line)
				if tc.optional {
					at := strings.IndexByte(line, ' ')
					line = line[:at+1] + "?" + line[at+1:]
				} else {
					line += " | required"
				}
				writerFields = append(writerFields, line)
			}
			writerSrc := prefix + strings.Join(writerFields, "\n") + "\n}\n"
			readerSrc := prefix + tc.fields + "\n}\n"
			w := kindsUnit{t: t, u: unitFrom(t, writerSrc)}
			w.m = tabletext.NewModel(w.u)
			r := kindsUnit{t: t, u: unitFrom(t, readerSrc)}
			r.m = tabletext.NewModel(r.u)
			wire := w.encode("Root", tc.text)
			expected := r.encode("Root", tc.text)
			if string(wire) == string(expected) {
				t.Fatal("presence witness did not store defaults")
			}
			probe := `package probe
 import("bytes";"testing";"unsafe")
 func region(n int64) []byte {raw:=make([]byte,n+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}
 func TestPresenceRemoval(t *testing.T){readVariable(t,"presence removal",[]byte(%q),[]byte(%q),true,false,true)}
 `
			runGenerated(t, readerSrc, fmt.Sprintf(probe, string(wire), string(expected))+nonCanonicalVariable("Root"))
			if tc.optional {
				plain := r.encode("Root", `{"tick":1,"scalar":7}`)
				present := w.encode("Root", `{"tick":1,"scalar":7}`)
				runGenerated(t, writerSrc, fmt.Sprintf(probe, string(plain), string(present))+nonCanonicalVariable("Root"))
			}
		})
	}
}
