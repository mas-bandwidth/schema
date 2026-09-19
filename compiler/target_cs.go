// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// csTarget emits C#.
type csTarget struct{}

func (csTarget) Names() []string { return []string{"cs", "csharp"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t csTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// cstable opens no file and a build ships a reader for every layout the lock
// records.
func (csTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	// Wide text and declared value defaults are carried on both wires.
	if err := refuseWideText(u, "cs"); err != nil {
		return nil, err
	}
	if err := refuseValueDefaults(u, "cs"); err != nil {
		return nil, err
	}
	files, err := csharp.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.cs per file — the
	// table-wire codecs and optional native surfaces; a table-free unit's
	// output is byte-identical to what the packet emitter alone produces
	tables, err := cstable.GenerateLineage(u, csTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table source; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// csTableLineage is the lock's lineage in the C# backend's own spelling. Nil in,
// nil out: a unit with no lock hands nothing, and every table then carries its
// own entry alone.
//
// THE RECORD SIZE IS THE COMPILER'S AND IS PASSED THROUGH. §5.2's record_bytes
// is the whole record — the eight hash bytes and then the body — and the value
// this leg emits for an older layout is exactly the one the shared lineage read
// reports for it. Adding eight here would make the C# leg disagree with every
// other leg the day the shared value changes, so the arithmetic stays in the one
// place that owns it (internal/lockfile, §5.9 #1) and this function is a
// translation and nothing else.
func csTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]cstable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]cstable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], cstable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Digest:  e.Digest,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return out
}

func init() {
	registerPacketValueDefaultCarrier("cs")
	registerWideTextCarrier("cs")
	registerBuiltin(csTarget{}, true, true, true, true)
	registerOptionalArrayCarrier("cs")
	registerMapCarrier("cs")
	registerListCarrier("cs")
}
