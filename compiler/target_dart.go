// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// dartTarget emits Dart 3.
type dartTarget struct{}

func (dartTarget) Names() []string { return []string{"dart"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t dartTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// darttable opens no file and a build ships a reader for every layout the lock
// records. Before this the driver called the plain `Generate`, so every real
// `schema generate --lang dart` shipped ONE known entry and a floor of 0 —
// §5.2's backward read was inert on this leg however long the committed lock's
// lineage was.
func (dartTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	// Packet wide text is carried; table kind 33 remains refused.
	if err := refuseWideText(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "dart"); err != nil {
		return nil, err
	}
	files, err := dart.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.dart per file — the
	// TABLE-wire codecs, the reflection descriptors and the text form
	// (docs/SPEC-TABLES.md); a table-free unit's output is byte-identical to what
	// the packet emitter alone produces
	tables, err := darttable.GenerateLineage(u, dartTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table library; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// dartTableLineage is the lock's lineage in the Dart backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing, and every table then
// carries its own entry alone.
//
// THE RECORD SIZE IS THE COMPILER'S AND IS PASSED THROUGH — §5.2's
// record_bytes is the whole record, the eight hash bytes and then the body, and
// the shared lineage read (internal/lockfile, §5.9 #1) is the one place that
// arithmetic lives. This function is a translation of six facts and nothing
// else; adding eight here would make the Dart leg disagree with every other leg
// the day the shared value changes.
func dartTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]darttable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]darttable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], darttable.FixedLineageEntry{
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
	registerPacketValueDefaultCarrier("dart")
	registerWideTextCarrier("dart")
	registerBuiltin(dartTarget{}, true, false, false, false)
}
