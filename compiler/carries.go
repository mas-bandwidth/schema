package compiler

import "github.com/mas-bandwidth/schema/v2/ir"

// These keys name the construct rules in docs/SPEC-TABLES.md. This card moves
// the carrier decision to one registration per target; the standard is unchanged.
const (
	carryTimestamp               = "timestamp"                 // §2.11
	carryRequired                = "required"                  // §2.3
	carryForms                   = "forms"                     // §2.12
	carryIntegrity               = "integrity"                 // §2.14
	carryUnboundedText           = "unbounded-text"            // §2.13.1
	carryTextListTotal           = "text-list-total"           // §2.13
	carryTextList                = "text-list"                 // §2.13
	carryArrayRoot               = "array-root"                // §16.10
	carryOneSidedRange           = "one-sided-range"           // §4.3
	carryOptionalVariableClosure = "optional-variable-closure" // §2.3
	carryOptionalMap             = "optional-map"              // §2.8
)

var carryConstructs = []string{
	carryTimestamp,
	carryRequired,
	carryForms,
	carryIntegrity,
	carryUnboundedText,
	carryTextListTotal,
	carryTextList,
	carryArrayRoot,
	carryOneSidedRange,
	carryOptionalVariableClosure,
	carryOptionalMap,
}

var knownCarryConstructs = func() map[string]struct{} {
	known := make(map[string]struct{}, len(carryConstructs))
	for _, construct := range carryConstructs {
		known[construct] = struct{}{}
	}
	return known
}()

// targetCarries records each target's constructs. A registration belongs in
// that target's carries_<target>.go file, beside its generator registration.
var targetCarries = map[string]map[string]struct{}{}

func registerCarries(target string, constructs ...string) {
	if target == "" {
		panic("compiler: cannot register carries for an empty target")
	}
	requireReservedWords(target)
	if _, exists := targetCarries[target]; exists {
		panic("compiler: carry registration already exists for target " + target)
	}
	for _, construct := range constructs {
		if _, known := knownCarryConstructs[construct]; !known {
			panic("compiler: unknown carried construct " + construct)
		}
	}
	set := make(map[string]struct{}, len(constructs))
	for _, construct := range constructs {
		set[construct] = struct{}{}
	}
	targetCarries[target] = set
}

// registerCarried adds one tagged feature to a target's existing registration,
// so the target's registerCarries call stays the one source of its identity.
// The construct keys it accepts are the construct rules of docs/SPEC-TABLES.md
// section 2 (the carry* constants). Go runs a package's init functions in
// file-name order, so a target's carries_<target>.go file (its registerCarries)
// runs before a carries_<target>_<feature>.go file (its registerCarried).
func registerCarried(target, construct string) {
	registerCarriedIn(targetCarries, target, construct)
}

// registerCarriedIn adds a construct to a target's existing registration in a
// registry. The construct keys it accepts are the construct rules of
// docs/SPEC-TABLES.md section 2 (the carry* constants). It panics when the
// target is empty or unregistered, when the construct is unknown, or when the
// construct is already registered for the target.
func registerCarriedIn(registry map[string]map[string]struct{}, target, construct string) {
	if target == "" {
		panic("compiler: cannot register a carried construct for an empty target")
	}
	if _, exists := registry[target]; !exists {
		panic("compiler: cannot add a carried construct to unregistered target " + target)
	}
	if _, known := knownCarryConstructs[construct]; !known {
		panic("compiler: unknown carried construct " + construct)
	}
	if _, exists := registry[target][construct]; exists {
		panic("compiler: carried construct already registered for target " + target + ": " + construct)
	}
	registry[target][construct] = struct{}{}
}

func carries(target, construct string) bool {
	_, ok := targetCarries[target][construct]
	return ok
}

// CarryConstructs is every construct key the registry knows, in the order
// docs/SPEC-TABLES.md's rules are listed above. The conformance harness reads
// it to name what a case uses (test/conformance/README.md, "The nightly").
func CarryConstructs() []string {
	return append([]string(nil), carryConstructs...)
}

// Carried is the set of constructs target registered, in registry order, and
// whether target has a registration in this build at all. A target behind a
// build tag (schema_leg_luau and its siblings) has none unless the binary was
// built with that tag, which is a different answer from "carries nothing".
func Carried(target string) ([]string, bool) {
	set, ok := targetCarries[target]
	if !ok {
		return nil, false
	}
	var out []string
	for _, construct := range carryConstructs {
		if _, carried := set[construct]; carried {
			out = append(out, construct)
		}
	}
	return out, true
}

// UnitConstructs is every registered construct unit u uses, in registry order:
// the same predicates [Compiler.Generate] refuses a target by, read once and
// named, so a port's claim over a unit is exactly "it carries every construct
// this returns" (docs/SPEC-TABLES.md §2.3, §2.8, §2.11-§2.14, §4.3, §16.10).
func UnitConstructs(u *ir.Unit) []string {
	_, _, required := firstRequiredField(u)
	_, forms := firstFormsTable(u)
	_, _, oneSided := firstOneSidedField(u)
	uses := map[string]bool{
		carryTimestamp:               len(ir.TableTimestampFields(u)) > 0,
		carryRequired:                required,
		carryForms:                   forms,
		carryIntegrity:               len(ir.TableIntegrityTables(u)) > 0,
		carryUnboundedText:           len(ir.TableUnboundedTexts(u)) > 0,
		carryTextListTotal:           len(ir.TableTextTotals(u)) > 0,
		carryTextList:                len(ir.TableTextLists(u)) > 0,
		carryArrayRoot:               len(ir.TableArrayRoots(u)) > 0,
		carryOneSidedRange:           oneSided,
		carryOptionalVariableClosure: len(ir.TableOptionalPointerClosures(u)) > 0,
		carryOptionalMap:             len(ir.OptionalMapFields(u)) > 0,
	}
	var out []string
	for _, construct := range carryConstructs {
		if uses[construct] {
			out = append(out, construct)
		}
	}
	return out
}
