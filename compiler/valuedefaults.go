// The STRING, BYTES and FLAGS DEFAULT's cross-target refusal (SPEC §4.2): the
// packet and table support are separate because a packet default initializes
// storage, while a table default also governs elision and absent-field reads.
// [refuseUnported] reaches the refusal for every port.
package compiler

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDefaultTargets is the canonical name of every built-in target whose
// backends carry a string, bytes or flags default on both wires.
//
// A PORT EARNS THIS LIST BY ANSWERING "ABSENT FIELD" WITH THE DECLARED VALUE
// on every table form it emits, and by writing the same value where its own
// form has a template. It is not a claim about the value surface: a port whose
// packet codec already spells the default (packetValueDefaultTargets) still
// initializes a fresh value correctly and can still be missing the table half.
var valueDefaultTargets = []string{"cpp", "c", "cs"}

// registerTableValueDefaultCarrier registers a port on the TABLE half of the
// list above. A port calls it from its own target file's init, beside the
// packet call, so a target stays one file (docs/CONTRIBUTING.md, "Adding a
// language").
func registerTableValueDefaultCarrier(name string) {
	valueDefaultTargets = append(valueDefaultTargets, name)
}

// packetValueDefaultTargets names the packet carriers independently of the
// table carriers. A port registers here from its own target file's init.
var packetValueDefaultTargets = []string{"cpp"}

func registerPacketValueDefaultCarrier(name string) {
	packetValueDefaultTargets = append(packetValueDefaultTargets, name)
}

// refuseValueDefaults is the named refusal every target without the form
// gives a unit whose fields carry a string, bytes or flags default (SPEC
// §4.2): a port that has not met one would initialize the field to empty or
// zero where the schema says otherwise, and on the table wire would elide the
// wrong value.
func refuseValueDefaults(u *ir.Unit, target string) error {
	names := ir.ValueDefaultFields(u)
	if len(names) == 0 {
		return nil
	}
	// A plain type can also supply table storage. Follow the same closure the
	// table emitter uses, including arrays, union arms, pointers and map entries.
	closure := ir.TableClosure(u)
	// THE FIXED FORM ELIDES NOTHING (docs/SPEC-TABLES.md §3.4): a record is a
	// constant-size body, so a declared default is the template's bytes and
	// the prefill's, and the Java fixed form carries a string, bytes or flags
	// default in both. So for Java the refusal is FORM 1's alone: a type that
	// only a fixed-form root reaches is not refused. A type a form-1 table
	// also reaches still is, because that table would elide it wrong. This is
	// a LOCAL scoping until the carrier list itself is lifted for every leg.
	fixedOnly := map[string]bool{}
	if target == "java" {
		fixedOnly = fixedFormOnlyClosure(u)
	}
	var tableNames []string
	for _, name := range names {
		owner, _, _ := strings.Cut(name, ".") // ValueDefaultFields returns Decl.field
		if closure[owner] && !fixedOnly[owner] {
			tableNames = append(tableNames, name)
		}
	}
	if len(tableNames) > 0 && !slices.Contains(valueDefaultTargets, target) {
		carry, flags := carriers(valueDefaultTargets)
		return fmt.Errorf("unit puts a string, bytes or flags default in a table closure (%s): table-wire defaults are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
			englishList(tableNames), englishList(carry), target, englishList(flags))
	}
	if slices.Contains(packetValueDefaultTargets, target) {
		return nil
	}
	carry, flags := carriers(packetValueDefaultTargets)
	return fmt.Errorf("unit declares a string, bytes or flags default (%s): packet-wire defaults are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
		englishList(names), englishList(carry), target, englishList(flags))
}

// fixedFormOnlyClosure is every declaration reached from a FIXED-FORM root
// (ir.TableFixedFormRoots) and from no other table of the unit: the set the
// fixed form alone gives storage to, walked the way ir.TableClosure walks —
// by value, as an array's element, and through a union's arms.
func fixedFormOnlyClosure(u *ir.Unit) map[string]bool {
	fixed := map[string]bool{}
	for _, st := range ir.TableFixedFormRoots(u) {
		fixed[st.Name] = true
	}
	reach := func(roots func(*ir.Struct) bool) map[string]bool {
		out := map[string]bool{}
		seenUnion := map[*ir.Union]bool{}
		var walk func(st *ir.Struct)
		var walkUnion func(un *ir.Union)
		walkUnion = func(un *ir.Union) {
			if seenUnion[un] {
				return
			}
			seenUnion[un] = true
			for _, v := range un.Variants {
				if v.F == nil || v.F.Type.Kind != ir.TNamed {
					continue
				}
				switch ref := v.F.Type.Ref.(type) {
				case *ir.Struct:
					walk(ref)
				case *ir.Union:
					walkUnion(ref)
				}
			}
		}
		walk = func(st *ir.Struct) {
			if out[st.Name] {
				return
			}
			out[st.Name] = true
			for _, f := range st.Fields {
				if f.IsMap() {
					walk(f.MapEntry)
					continue
				}
				if f.Type.Kind != ir.TNamed {
					continue
				}
				switch ref := f.Type.Ref.(type) {
				case *ir.Struct:
					walk(ref)
				case *ir.Union:
					walkUnion(ref)
				}
			}
		}
		for _, f := range u.Files {
			for _, st := range f.Tables {
				if st.IsTable && roots(st) {
					walk(st)
				}
			}
		}
		return out
	}
	fromFixed := reach(func(st *ir.Struct) bool { return fixed[st.Name] })
	fromOthers := reach(func(st *ir.Struct) bool { return !fixed[st.Name] })
	for name := range fromFixed {
		if fromOthers[name] {
			delete(fromFixed, name)
		}
	}
	return fromFixed
}
