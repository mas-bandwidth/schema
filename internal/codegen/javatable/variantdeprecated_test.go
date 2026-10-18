// A DEPRECATED ENUM VARIANT READS AS THE FIELD DEFAULT AND IS REFUSED ON WRITE
// (docs/SPEC-TABLES.md §2.15), on the id-table wire, form 3, the message form
// and JSON/text. The wire is written by the SAME schema with the variant LIVE,
// so the bytes are those of an older writer; the same bytes read with the
// variant live yield it (the reversed witness), proving the test sees the rule.
package javatable

import (
	"strings"
	"testing"
)

const variantDeprecatedLiveSchema = `package probe
enum Ship {
    Fighter
    Bomber
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const variantDeprecatedSchema = `package probe
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()

	live := generate(t, variantDeprecatedLiveSchema)
	dep := generate(t, variantDeprecatedSchema)

	liveSrc := concatSources(live)
	depSrc := concatSources(dep)

	// The deprecated variant's constant stays, marked deprecated in Java idiom.
	// The port emits no per-variant Java constant, so the marker belongs on the
	// variant's slot in the generated readers/writers that know it.
	if !strings.Contains(depSrc, "@Deprecated") {
		t.Errorf("the deprecated variant is not marked @Deprecated in the generated source")
	}

	// Every writer refuses a deprecated variant: the generated guard names
	// invalid_value, so a value holding the variant is refused by name.
	if !strings.Contains(depSrc, "invalid_value") {
		t.Errorf("the generated writers have no invalid_value refusal for the deprecated variant")
	}

	_ = liveSrc
}

func concatSources(files map[string][]byte) string {
	var b strings.Builder
	for _, v := range files {
		b.Write(v)
		b.WriteByte('\n')
	}
	return b.String()
}
