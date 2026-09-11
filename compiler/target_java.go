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

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t javaTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// javatable opens no file and a build ships a reader for every layout the lock
// records.
func (javaTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
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
	tables, err := javatable.GenerateLineage(u, javaTableLineage(u, lineage))
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

// javaTableLineage is the lock's lineage in the Java backend's own spelling. Nil
// in, nil out: a unit with no lock hands nothing.
//
// THERE IS NO CONVERSION. §5.2's `R.known[i].record_bytes` is the WHOLE record —
// `8 + C(root)`, the hash and then the body — which is what javatable's own
// entry carries and what its reader divides the tail by. The LOCK records a BODY
// size (`ir.TableFixedTypeBytes`, internal/lockfile/lineage.go), and #929 moved
// the one addition of the eight hash bytes into [openFixedLineage] so that it is
// made ONCE, for every backend the driver hands entries to
// (compiler/lineage.go, TestFixedLineageRecordSizeIsTheWholeRecord). So
// `e.Record` here is ALREADY the whole record; adding eight again would ship
// every older entry of a LOCKED unit sixteen bytes long, and step 9 divides the
// tail behind the layout by that number.
func javaTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]javatable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]javatable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], javatable.FixedLineageEntry{
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
	// The id-table wire (§3) — FORM 1, the one this refusal was written for —
	// is NOT emitted for this backend at all, so its elision rule is not this
	// port's to carry today; the day that codec lands here it has to keep this
	// claim true. Registered the way go and rust register (target_go.go,
	// target_rust.go): the append IS the registration.
	valueDefaultTargets = append(valueDefaultTargets, "java")
	registerWideTextCarrier("java")
	registerBuiltin(javaTarget{}, true, false, false, false)
}
