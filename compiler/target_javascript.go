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

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t jsTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// jstable opens no file and a build ships a reader for every layout the lock
// records.
func (jsTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	// Packet wide text is carried; table kind 33 remains refused.
	if err := refuseWideText(u, "js"); err != nil {
		return nil, err
	}
	// THE TABLE-SIDE UNION IS CARRIED, NAMED PER TABLE, NOT REFUSED PER UNIT
	// (docs/SPEC-TABLES.md §2.6, §3.4): the fixed form walks a union's arms and
	// this backend refuses only the tables its own closure test names
	// (internal/codegen/jstable/fixedRefusal, fixedRoots), so a unit that
	// declares a union with table arms keeps every table the form reaches — the
	// `backend` unit's three message roots among them — instead of losing the
	// whole unit to one union. The refusal that remains is this leg's own: an
	// array of unions, which neither of its table wires carries. A byte buffer
	// (*bytes, *string) is carried by the variable wire (docs/SPEC-TABLES.md
	// §2.5), so refuseBytes and refuseStrings pass for js (the blobs flag below).
	if err := refuseValueDefaults(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseWasRows(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseUnionArrays(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseBytes(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseStrings(u, "js"); err != nil {
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
	tables, err := jstable.GenerateLineage(u, jsTableLineage(u, lineage))
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

// jsTableLineage is the lock's lineage in the JavaScript backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing.
func jsTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]jstable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]jstable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], jstable.FixedLineageEntry{
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
	registerListCarrier("js")
	// The variable wire carries BOTH blobs (§2.5) — *string and *bytes — so js
	// registers as a full byte-buffer carrier through registerBuiltin's blobs
	// flag, which lifts refuseBytes and refuseStrings in GenerateLineage (§11).
	// It carries a union with table arms and an array of unions too (§2.6, §3),
	// so the arms and unionArrays flags are set, and refuseUnionArrays answers
	// nil for it.
	registerBuiltin(jsTarget{}, true, true, true, true)
	// MAPS (§2.8) and OPTIONAL ARRAYS (§2.3) ride the variable wire
	// (internal/codegen/jstable variablearms.go): refuseMaps and
	// refuseOptionalArrays answer nil for js. A table the variable wire still
	// does not carry is refused BY NAME per table in jstable (§11), never here
	// per unit.
	registerMapCarrier("js")
	registerOptionalArrayCarrier("js")
}
