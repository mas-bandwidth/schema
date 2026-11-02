package cstable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"strings"
	"testing"
)

// Long, shallow JSON must use constant stack space. Repeated keys also pin the
// exact duplicate count and last-wins value against the independent model.
func TestJsonHighKeyCountCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	const count = 100000
	var fields, keyed, skipped, mapped, flags strings.Builder
	for i := range count {
		if i > 0 {
			fields.WriteByte(',')
			keyed.WriteByte(',')
			skipped.WriteByte(',')
			mapped.WriteByte(',')
			flags.WriteByte(',')
		}
		fmt.Fprintf(&fields, `"value":%d`, i)
		fmt.Fprintf(&keyed, `"A":%d`, i)
		fmt.Fprintf(&skipped, `"x":%d`, i)
		fmt.Fprintf(&mapped, `"x":%d`, i)
		flags.WriteString(`"Jump"`)
	}
	runCarryCS(t, `package probe
enum Key { A, B }
flags Caps { Jump, Run }
table Root {
 value int32
 values [Key]int32
 caps Caps
 items map[string(8)]int32
}
`, []string{
		"{" + fields.String() + "}",
		`{"unknown":{` + skipped.String() + "}}",
		`{"values":{` + keyed.String() + "}}",
		`{"caps":[` + flags.String() + "]}",
		`{"items":{` + mapped.String() + "}}",
	})
}
