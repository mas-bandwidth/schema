// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// elixirTarget emits Elixir 1.20.
type elixirTarget struct{}

func (elixirTarget) Names() []string { return []string{"elixir"} }

func (elixirTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// Packet wide text is carried; table kind 33 remains refused.
	if err := refuseWideText(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "elixir"); err != nil {
		return nil, err
	}
	files, err := elixir.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.ex per file — the
	// TABLE-wire codecs, the reflection descriptors, the text form and the two
	// accelerators' READ side (docs/SPEC-TABLES.md); a table-free unit's output
	// is byte-identical to what the packet emitter alone produces.
	tables, err := elixirtable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file whose basename collides with a generated table module; rename one file (docs/SPEC-TABLES.md §11)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerPacketValueDefaultCarrier("elixir")
	// AND THE TABLE HALF (SPEC §4.2). The three table forms this backend emits
	// each answer "absent field" with the declared value:
	//
	//   - the FIXED form (§3.4) has one place an absent field is answered, the
	//     PREFILL, and the prefill is this type's declared defaults laid out as
	//     record bytes (internal/codegen/elixirtable/fixeddefaults.go) — a
	//     string, bytes or flags default among them. It is the WRITE template
	//     too, so the same bytes are what a writer stores over.
	//   - the BLOCK (§19) and COOKED (§7) forms are read halves over a DENSE
	//     image: every field of the image is present by construction, so there
	//     is no absent field for a default to answer and nothing to elide.
	//
	// The id-table wire (§3) is NOT emitted for this backend (schema#515), so
	// its elision rule is not this port's to carry today; when #515 lands, the
	// codec it brings has to keep this claim true.
	registerTableValueDefaultCarrier("elixir")
	registerWideTextCarrier("elixir")
	registerBuiltin(elixirTarget{}, true, false, false, false)
}
