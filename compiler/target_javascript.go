// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language"). The
// file is named for the alias, because Go reads a _js suffix as a GOOS build
// constraint and would leave target_js.go out of every other build.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/jstable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsTarget emits JavaScript ES modules.
type jsTarget struct{}

func (jsTarget) Names() []string { return []string{"js", "javascript"} }

func (jsTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// Packet wide text is carried; table kind 33 remains refused.
	if err := refuseWideText(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "js"); err != nil {
		return nil, err
	}
	files, err := js.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.js per file — the
	// TABLE-wire codecs, FIXED class — plus the two accelerators'
	// <Base>Block.js and <Base>Cook.js READERS (docs/SPEC-TABLES.md); a
	// table-free unit's output is byte-identical to what the packet emitter
	// alone produces
	tables, err := jstable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table module; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	// THE TABLE WIRE CARRIES A STRING, BYTES OR FLAGS DEFAULT TOO (SPEC §4.2),
	// and the packet registration below is not enough to say so: a table
	// default governs ELISION and the ABSENT-FIELD READ as well as storage.
	// Form 3 elides nothing — every field of every record rides — so what is
	// left is the absent-field read, and on this form that is the PREFILL,
	// which internal/codegen/jstable lays down as a constant run of bytes:
	// a string's or bytes' declared default with its length ahead of it, and a
	// flags default at its storage width (fixedprefill.go). The write side
	// takes the value's own storage, which the packet constructor has
	// initialized from the same defaults since js registered below.
	//
	// It is claimed here rather than assumed: the FX1/FX2 leg carries
	// `label string(8) = "fx"` and its bytes are compared against the C++
	// reference's, so a default this port dropped would be a byte that did not
	// come back.
	valueDefaultTargets = append(valueDefaultTargets, "js")
	registerPacketValueDefaultCarrier("js")
	registerWideTextCarrier("js")
	registerBuiltin(jsTarget{}, true, false, false, false)
}
