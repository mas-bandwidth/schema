package compiler

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// builtinTargets is every built-in generator, each registered by its own
// file's init — target_<lang>.go — so a target is one file and a new one adds
// a file (docs/CONTRIBUTING.md, "Adding a language"). The per-language
// emitters stay internal — they are implementations, not API, and freezing
// nine emitter packages under semver would buy nothing — but they reach the
// driver through [Generator] and nothing else, which is what makes that
// interface a door rather than a decoration: if a built-in target can be
// expressed as a registered generator, so can anyone's.
var builtinTargets []Generator

// tableTargets is the canonical name of every built-in target that carries a
// table backend (docs/SPEC-TABLES.md); refuseTables names them.
var tableTargets []string

// tableArmTargets is the canonical name of every built-in target whose table
// backend carries a union with TABLE arms (docs/SPEC-TABLES.md §2.6);
// refuseTableArms names them.
var tableArmTargets []string

// unionArrayTargets is the canonical name of every built-in target whose
// table backend carries an ARRAY OF UNIONS (docs/SPEC-TABLES.md §2.6);
// refuseUnionArrays names them.
var unionArrayTargets []string

// registerBuiltin is what a target's file calls from its init. tables says
// whether the target emits table sources; one that does not refuses a unit
// declaring tables through refuseTables, and is named there as a follow-on.
// arms says whether those sources carry a union with table arms; one that
// does not refuses a unit declaring one through refuseTableArms, the same way.
// unionArrays says whether they carry an array of unions in a table closure;
// one that does not refuses through refuseUnionArrays.
func registerBuiltin(g Generator, tables, arms, unionArrays bool) {
	builtinTargets = append(builtinTargets, g)
	if tables {
		tableTargets = append(tableTargets, g.Names()[0])
	}
	if arms {
		tableArmTargets = append(tableArmTargets, g.Names()[0])
	}
	if unionArrays {
		unionArrayTargets = append(unionArrayTargets, g.Names()[0])
	}
}

// builtins is the set [New] registers, in registration order.
func builtins() []Generator {
	return builtinTargets
}

// refuseTables is the named refusal every target without a table backend
// gives a unit that declares tables (docs/SPEC-TABLES.md): the targets that
// carry one are named, and each remaining per-language backend is a named
// follow-on — refused loudly here rather than silently emitting a unit with
// the tables missing.
func refuseTables(u *ir.Unit, target string) error {
	if len(u.Tables) == 0 {
		return nil
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	carry, flags := carriers(tableTargets)
	return fmt.Errorf("unit declares tables (%s) — tables are %s only today, and the %s table backend is a named follow-on; generate with %s, or move the tables to their own unit (docs/SPEC-TABLES.md)",
		englishList(names), englishList(carry), target, englishList(flags))
}

// refuseTableArms is the named refusal every target without the form gives a
// unit whose union has a TABLE arm (docs/SPEC-TABLES.md §2.6, §11): the
// targets that carry it are named, and each remaining port's is a named
// follow-on — refused loudly here rather than emitted as a union naming a
// type the port never declares.
func refuseTableArms(u *ir.Unit, target string) error {
	if len(u.TableUnions) == 0 {
		return nil
	}
	names := make([]string, 0, len(u.TableUnions))
	for name := range u.TableUnions {
		names = append(names, name)
	}
	sort.Strings(names)
	carry, flags := carriers(tableArmTargets)
	return fmt.Errorf("unit declares a union whose arm is a table (%s) — a union with table arms is %s only today, and the %s form is a named follow-on; generate with %s, or move the union and its tables to their own unit (docs/SPEC-TABLES.md §2.6, §11)",
		englishList(names), englishList(carry), target, englishList(flags))
}

// refuseUnionArrays is the named refusal every target without the form gives
// a unit whose table closure holds an ARRAY OF UNIONS (docs/SPEC-TABLES.md
// §2.6, §11): the targets that carry it are named, and each remaining port's
// is a named follow-on — refused loudly here rather than emitted as a
// fixed-class codec that never met the element.
func refuseUnionArrays(u *ir.Unit, target string) error {
	fields := ir.TableUnionArrays(u)
	if len(fields) == 0 {
		return nil
	}
	carry, flags := carriers(unionArrayTargets)
	return fmt.Errorf("unit declares an array of unions in a table closure (%s) — an array of unions is %s only today, and the %s form is a named follow-on; generate with %s, or wrap the union in a table and declare an array of that (docs/SPEC-TABLES.md §2.6, §11)",
		englishList(fields), englishList(carry), target, englishList(flags))
}

// carriers is the registered targets that carry a form, sorted, beside the
// --lang flag that selects each — what a refusal names as the way through.
func carriers(targets []string) (carry, flags []string) {
	carry = append([]string(nil), targets...)
	sort.Strings(carry)
	flags = make([]string, len(carry))
	for i, t := range carry {
		flags[i] = "--lang " + t
	}
	return carry, flags
}
