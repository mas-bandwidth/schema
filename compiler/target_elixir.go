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

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read, or
// that has none. Every fixed table then carries the single entry it can always
// compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1, #2).
func (t elixirTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// elixirtable opens no file and a build ships a reader for every layout the lock
// records.
func (elixirTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	// Packet wide text is carried, and table kind 33 too: elixir is in
	// tableWideTextTargets (compiler/widetext.go, docs/SPEC-TABLES.md §3).
	if err := refuseWideText(u, "elixir"); err != nil {
		return nil, err
	}
	// refuseUnported, split per construct. This target registers as a carrier
	// of maps (§2.8), optional arrays (§2.3), table arms and arrays of unions
	// (§2.6) in init below, so those refusals pass by registration; the calls
	// stay so a carrier that is unregistered is refused by name again (§11).
	if err := refuseValueDefaults(u, "elixir"); err != nil {
		return nil, err
	}
	// docs/SPEC-TABLES.md §2.6, §3 and §5: the variable form's arm header
	// hashes a selected arm's WireName (internal/codegen/elixirtable's
	// armDescriptor), so a `was` on an arm of a TABLE union is carried. A unit
	// with any other `was` row — an enum variant, a packet union's arm, a
	// type's field — still takes refuseWasRows unchanged.
	if !elixirCarriesTableArmWasOnly(u) {
		if err := refuseWasRows(u, "elixir"); err != nil {
			return nil, err
		}
	}
	// refuseVoidArms is retired here on purpose (docs/SPEC-TABLES.md §2.6, §3's
	// kind-32 paragraph): the variable form carries a PAYLOAD-FREE arm as the
	// arm id under kind 32 with L = 0, its value the arm's name alone, and
	// reads L != 0 there as malformed (internal/codegen/elixirtable,
	// TestPayloadFreeArmsOnTheTableWireElixir). The fixed form's shared
	// eligibility walk still declines a union with one (ir/fixedform.go), so
	// such a root takes the variable form rather than a mis-encoded record.
	if err := refuseBytes(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseStrings(u, "elixir"); err != nil {
		return nil, err
	}
	if !elixirListsBesideUncarried(u) {
		if err := refuseTableArms(u, "elixir"); err != nil {
			return nil, err
		}
		if err := refuseUnionArrays(u, "elixir"); err != nil {
			return nil, err
		}
		if err := refuseMaps(u, "elixir"); err != nil {
			return nil, err
		}
	}
	if err := refuseOptionalArrays(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "elixir"); err != nil {
		return nil, err
	}
	files, err := elixir.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get Table.ex, one variable-form codec per
	// unit, beside <Base>Block.ex and <Base>Cook.ex (docs/SPEC-TABLES.md); a
	// table-free unit's output is byte-identical to what the packet emitter
	// alone produces.
	tables, err := elixirtable.GenerateLineage(u, elixirTableLineage(u, lineage))
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

// elixirCarriesTableArmWasOnly reports whether every `was` row ir.WasRows
// would name sits on an arm of a table union (docs/SPEC-TABLES.md §2.6, §5):
// false at the first enum-variant, type-field or packet-union `was`.
func elixirCarriesTableArmWasOnly(u *ir.Unit) bool {
	for _, e := range u.Enums {
		for _, old := range e.Was {
			if old != "" {
				return false
			}
		}
	}
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if f.WasName != "" {
				return false
			}
		}
	}
	for _, un := range u.Unions {
		for _, arm := range un.Variants {
			if arm.WasName != "" {
				return false
			}
		}
	}
	return true
}

// elixirListsBesideUncarried reports whether the unit declares an unbounded
// array this backend emits (docs/SPEC-TABLES.md §2.9). Such a unit still
// generates when a map or a union sits beside those lists: the lists are the
// carried wire, and the other roots are uncarried rather than a refusal of
// the whole unit (§2.6, §2.8, §11).
func elixirListsBesideUncarried(u *ir.Unit) bool {
	return len(ir.ListFields(u)) > 0
}

// elixirTableLineage is the lock's lineage in the Elixir backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing, and the entry type is
// elixirtable's — the same five facts, spelled once per backend (target_go.go's
// goTableLineage is the shape this copies).
func elixirTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]elixirtable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]elixirtable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], elixirtable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return out
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
	// The id-table wire (§3) is emitted for this backend for the whole variable
	// class (docs/SPEC-TABLES.md §2, §3); its codec elides a field at its
	// declared default, so the elision claim stays true as the class widens.
	// Registered the way go, java and rust register (target_go.go,
	// target_java.go, target_rust.go): the append IS the registration.
	valueDefaultTargets = append(valueDefaultTargets, "elixir")
	registerWideTextCarrier("elixir")
	registerListCarrier("elixir")
	registerMapCarrier("elixir")
	registerOptionalArrayCarrier("elixir")
	// arms=true and unionArrays=true: the variable form carries a union whose
	// arms are field lines of any type, payload-free arms among them, and an
	// array of unions (docs/SPEC-TABLES.md §2.6, §3). blobs=true: it carries
	// *string and *bytes (§2.5), so the compiler-level byte-buffer gate must
	// not fire for the two blobs the wire rides as nodes.
	registerBuiltin(elixirTarget{}, true, true, true, true)
}
