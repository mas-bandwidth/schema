// Package tablenames is THE registry of unit-level names the generated
// TABLE-wire runtimes define (docs/SPEC-TABLES.md §11).
//
// It exists because the same list is needed in two places that must never
// disagree: the CHECKER claims these names, so no legal schema can reach a
// generated source that does not compile; and the EMITTERS define them. A name
// defined by one and unclaimed by the other is precisely the defect §11
// promises cannot happen — a legal schema whose generated code a compiler
// rejects — so the two read one list instead of keeping two.
//
// WHERE THE CHECKER CLAIMS EACH NAME, which is the one thing this list is read
// for: the entries the generated VIEW FILE spells (View, below) are claimed in
// EVERY unit, because a table-free unit's view defines them (§8.2) and a name
// free today would be a collision the day that view is emitted; every other
// entry is claimed in a unit that DECLARES A TABLE and nowhere else, because
// the generated table sources are the only place it is written. §11 states the
// split, docs/SPEC.md §4.6 points a packet-only author at it, and
// ClaimedInEveryUnit and ClaimedWithATable are the two halves the checker
// reads. A packet-only unit keeps `type Announce`, `const ok` and every other
// spelling of the announcement and refusal vocabulary.
//
// The list is FRONT-END LAW, not one target's inventory: every backend's
// spelling is claimed for ALL of them. A unit legal under one target and
// illegal under another is the trap the claim exists to prevent, so C++'s
// snake_case float helpers and C#'s CamelCase ones are claimed together, and
// so are the variable-length runtime's names — claimed whenever a unit
// declares a table, not only when one carries pointers, so adding a pointer to
// an existing table never turns a legal declaration elsewhere into a collision.
//
// ADDING A RUNTIME NAME. Register it in the file of every backend that defines
// it: <lang>.go, one per backend, each a define call listing what that
// backend's emitted text carries. A backend is one file, and a new one adds a
// file (docs/CONTRIBUTING.md, "Adding a language").
// The emitters are not generated from this list — a runtime is prose-laden
// source that reads better written out — so the registry is held honest from
// the other side instead: compiler's TestTableRuntimeNamesAreClaimed scans the
// emitted output for every Table* identifier and requires the set to equal
// this registry's, in both directions. An emitted name nobody registered fails
// there; so does a registered name nothing emits any more.
package tablenames

import (
	"fmt"
	"sort"
)

// Backend is a bit per table backend, so one entry can say which of them
// define the name. Each backend's own file declares its bit (Cpp is 1 << 0,
// Cs is 1 << 1, and so on), and define refuses a bit two files claim.
type Backend uint16

// Name is one spelling the generated table runtimes carry, and the backends
// whose emitted text carries it.
type Name struct {
	Name string
	By   Backend
	What string // what it is, for a reader of this file

	// Scoped marks a spelling NO DECLARATION CAN COLLIDE WITH, so it claims
	// nothing: a member reached through its owner, or — in Rust — an item
	// private to the shared runtime module, which the crate root never sees.
	// Registered anyway, so the scan that holds this list honest has an answer
	// for every name it finds rather than a filter nobody can see.
	Scoped bool

	// View marks a spelling the generated VIEW FILE itself carries, which is
	// the whole of what makes a runtime name claimable in a unit that
	// declares no table.
	//
	// docs/SPEC-TABLES.md:560 gives ONE reason for an every-unit claim: a
	// table-free unit's view file DEFINES the name (§8.2), so a name free
	// today is a collision the day that view is emitted. That reason reaches
	// the DESCRIPTOR SURFACE the view spells and stops there. Everything else
	// in this registry, meaning the announcement vocabulary (§3.3), the
	// refusal vocabulary (§6.5, §7, §19.2), the accelerators' runtimes and
	// the cooked form's names, is written into the generated TABLE sources
	// and into no view file, so it is claimed where those sources are
	// written: in a unit that declares a table (§11).
	//
	// The set is MEASURED, not asserted: compiler's
	// TestViewFileNamesAreClaimedInEveryUnit scans the emitted view files for
	// every registered spelling and requires the set it finds to be exactly
	// this one. A view file that grows a name nobody marked fails there, and
	// so does a mark no view file needs. That is the same instrument, in the
	// same direction, that holds the registry itself honest.
	View bool

	// RustConst marks a name the Rust backend spells as a CRATE-SCOPE
	// CONSTANT, whose emitted identifier is therefore ir.RustConstName of the
	// name rather than the name itself.
	//
	// It exists because Rust's constant spelling is MANY-TO-ONE: a schema
	// const named TableCookMagic, TABLE_COOK_MAGIC or table_cook_magic all
	// lower to one crate-scope TABLE_COOK_MAGIC, and claiming the registered
	// spelling alone leaves the other two legal. What they generate is a pair
	// of ambiguous glob re-exports — the generated crate builds with a
	// warning, the user's own constant is silently shadowed at the crate root,
	// and any CONSUMER that names the symbol fails to compile under
	// ambiguous_glob_imports, which is deny-by-default and
	// future-incompatible. internal/check claims these in the MAPPED space,
	// beside the same claim it already makes between two user declarations
	// ("both generate the symbol MAX_HEALTH").
	RustConst bool
}

// All returns the whole registry, sorted by name.
func All() []Name {
	out := append([]Name(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Claimed is every UNIT-LEVEL registered name, sorted — what the checker
// claims when a unit declares a table, whatever target it is generating for.
// Scoped spellings are not claimed: they cannot collide with a declaration.
//
// The claim is made in TWO scopes and this is their union:
// ClaimedInEveryUnit is claimed with or without a table, ClaimedWithATable
// only beside one, and every unit-level name is in exactly one of the two.
func Claimed() []string {
	var names []string
	for _, n := range registry {
		if !n.Scoped {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// ClaimedInEveryUnit is every unit-level registered name the generated VIEW
// FILE spells, sorted: the names claimed whether or not a unit declares a
// table, because a table-free unit's view defines them (docs/SPEC-TABLES.md
// §8.2, §11).
func ClaimedInEveryUnit() []string {
	var names []string
	for _, n := range registry {
		if !n.Scoped && n.View {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// ClaimedWithATable is every unit-level registered name no view file spells,
// sorted: the names the generated TABLE sources define, claimed in a unit
// that declares a table and in no other (docs/SPEC-TABLES.md §11).
func ClaimedWithATable() []string {
	var names []string
	for _, n := range registry {
		if !n.Scoped && !n.View {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// InEveryUnit reports whether the view file spells name, which is what
// decides the scope of its claim. False for a name nothing registers.
func InEveryUnit(name string) bool {
	for _, n := range registry {
		if n.Name == name {
			return n.View
		}
	}
	return false
}

// DefinedBy is every name one backend's emitted text carries, scoped ones
// included, sorted.
func DefinedBy(b Backend) []string {
	var names []string
	for _, n := range registry {
		if n.By&b != 0 {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// RustConstants is every registered name the Rust backend spells as a
// crate-scope CONSTANT, sorted. The caller maps each through
// ir.RustConstName to get the emitted identifier; this package does not, so
// that it keeps depending on nothing.
func RustConstants() []string {
	var names []string
	for _, n := range registry {
		if n.RustConst && n.By&Rust != 0 {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// Registered reports whether a spelling is in the registry at all — the
// question the emitted-text scan asks of every Table* identifier it finds.
func Registered(name string) bool {
	for _, n := range registry {
		if n.Name == name {
			return true
		}
	}
	return false
}

// By is the set of backends that define name, or 0 when nothing does.
//
// A diagnostic that names a lowercase claim needs this: C and Go both put
// lowercase runtime names at unit scope, for different reasons — a Go package
// is one namespace, and C has no namespace at all — and a message that guessed
// from the spelling would tell half of its readers about the wrong language.
func By(name string) Backend {
	for _, n := range registry {
		if n.Name == name {
			return n.By
		}
	}
	return 0
}

// registry is the whole list, built by every backend's own file at init and
// merged by name: a name two backends define is one entry whose By is the
// union. Sorted by nothing in particular — callers that need an order sort for
// themselves, because map and slice order must never decide what a diagnostic
// says.
var registry []Name

// claimed is every backend bit a file has defined, so two files cannot share
// one.
var claimed Backend

// define is how a backend's file registers the names its emitted text carries.
// A name another backend already registered gains this backend's bit. Its
// What stays the lowest bit's, so init order decides nothing; Scoped holds
// only if EVERY definer scopes it, because the claim is the union over the
// backends and one unit-level spelling claims the name for all of them;
// RustConst and View are kept whichever definer set them.
func define(b Backend, names ...Name) {
	if b == 0 || b&(b-1) != 0 {
		panic(fmt.Sprintf("tablenames: %b is not one backend bit", b))
	}
	if claimed&b != 0 {
		panic(fmt.Sprintf("tablenames: backend bit %b is defined twice — take the next free one", b))
	}
	claimed |= b
	for _, n := range names {
		n.By = b
		i := -1
		for j := range registry {
			if registry[j].Name == n.Name {
				i = j
				break
			}
		}
		if i < 0 {
			registry = append(registry, n)
			continue
		}
		have := &registry[i]
		if b < have.By&-have.By {
			have.What = n.What
		}
		have.By |= b
		have.Scoped = have.Scoped && n.Scoped
		have.RustConst = have.RustConst || n.RustConst
		have.View = have.View || n.View
	}
}
