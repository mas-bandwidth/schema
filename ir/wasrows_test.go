// The second `was` row against the two ids (docs/VERSIONING.md promise 4): a
// variant, an arm or a type's field renamed under `was` moves neither id, and
// the same rename bare moves both where a type reaches the declaration.
package ir_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const wasRowsBaseSource = `package demo

enum Grade { Bronze, Silver, Gold }

type Buff
{
    multiplier float32 = 1.0
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    ward Ward
    ping
}

type Packet
{
    grade  Grade
    effect Effect
    buff   Buff
}

table Cfg
{
    grade  Grade
    effect Effect
    buff   Buff
}
`

func TestVocabularyRenamesUnderWasMoveNeitherId(t *testing.T) {
	base := unitFrom(t, wasRowsBaseSource)
	for _, tc := range []struct{ name, old, was, bare string }{
		{"an enum variant", "enum Grade { Bronze, Silver, Gold }", "enum Grade\n{\n    Bronze,\n    Argent | was = \"Silver\"\n    Gold\n}", "enum Grade { Bronze, Argent, Gold }"},
		{"a union arm", "    ward Ward\n", "    shield Ward | was = \"ward\"\n", "    shield Ward\n"},
		{"a payload-free arm", "    ping\n", "    pong | was = \"ping\"\n", "    pong\n"},
		{"a type's field", "multiplier float32 = 1.0", "mult float32 = 1.0 | was = \"multiplier\"", "mult float32 = 1.0"},
	} {
		under := unitFrom(t, strings.Replace(wasRowsBaseSource, tc.old, tc.was, 1))
		if under.ProtocolId != base.ProtocolId {
			t.Errorf("%s renamed under was moved the protocol id", tc.name)
		}
		if ir.BuildVersion(under) != ir.BuildVersion(base) {
			t.Errorf("%s renamed under was moved the build version:\n%s", tc.name, ir.CookProjection(under))
		}
		// the DISCRIMINATION control: the bare rename moves both, because
		// Packet reaches every declaration and names ride the projection
		bare := unitFrom(t, strings.Replace(wasRowsBaseSource, tc.old, tc.bare, 1))
		if bare.ProtocolId == base.ProtocolId {
			t.Errorf("%s renamed bare did not move the protocol id", tc.name)
		}
		if ir.BuildVersion(bare) == ir.BuildVersion(base) {
			t.Errorf("%s renamed bare did not move the build version", tc.name)
		}
	}
}
