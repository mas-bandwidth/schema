// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ctable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cTarget emits C99 (SPEC §6.1).
type cTarget struct{}

func (cTarget) Names() []string { return []string{"c"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t cTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// ctable opens no file and a build ships a reader for every layout the lock
// records.
func (cTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	// C carries packet and table wide text.
	if err := refuseWideText(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "c"); err != nil {
		return nil, err
	}
	files, err := cgen.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.h / <Base>Table.c per
	// file — the TABLE-wire codecs (docs/SPEC-TABLES.md); a table-free unit's
	// output is byte-identical to what the packet emitter alone produces
	tables, err := ctable.GenerateLineage(u, cTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table header; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// cTableLineage is the lock's lineage in the C backend's own spelling. Nil in,
// nil out: a unit with no lock hands nothing.
func cTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]ctable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]ctable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], ctable.FixedLineageEntry{
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
	registerWideTextCarrier("c")
	registerBuiltin(cTarget{}, true, true, true, true)
	registerOptionalArrayCarrier("c")
	registerListCarrier("c")
	registerMapCarrier("c")
	registerPacketValueDefaultCarrier("c")
}
