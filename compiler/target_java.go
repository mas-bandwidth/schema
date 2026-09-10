// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/javatable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// javaTarget emits Java 17.
type javaTarget struct{}

func (javaTarget) Names() []string { return []string{"java"} }

func (javaTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// Packet wide text is carried; table kind 33 remains refused.
	if err := refuseWideText(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "java"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "java"); err != nil {
		return nil, err
	}
	files, err := java.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.java per file plus the
	// unit's shared runtime, one PUBLIC TYPE PER FILE — the TABLE-wire codecs,
	// FIXED class (docs/SPEC-TABLES.md); a table-free unit's output is
	// byte-identical to what the packet emitter alone produces
	tables, err := javatable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file, a table runtime type and a declaration's generated name all land in the Java package's one-public-class-per-file namespace; rename one of them (docs/SPEC-TABLES.md §11)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerPacketValueDefaultCarrier("java")
	// AND THE TABLE HALF (SPEC §4.2). The two table forms this backend emits
	// each answer "absent field" with the declared value:
	//
	//   - the FIXED form (§3.4) has one place an absent field is answered, the
	//     PREFILL, and the prefill is this type's declared defaults laid out as
	//     record bytes (internal/codegen/javatable/fixedform.go,
	//     fixedDefaultImage) — a string or bytes default behind its length, a
	//     flags default as its mask. It is the WRITE template too, so the same
	//     bytes are what a writer stores over, and the generated value class
	//     constructs the same values beside it.
	//   - the BLOCK (§19) and COOKED (§7) forms are read halves over a DENSE
	//     image: every field of the image is present by construction, so there
	//     is no absent field for a default to answer and nothing to elide.
	//
	// The id-table wire (§3) is NOT emitted for this backend, so its elision
	// rule is not this port's to carry today; the day that codec lands here it
	// has to keep this claim true.
	registerTableValueDefaultCarrier("java")
	registerWideTextCarrier("java")
	registerBuiltin(javaTarget{}, true, false, false, false)
}
