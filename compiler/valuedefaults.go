// The STRING, BYTES and FLAGS DEFAULT's cross-target refusal (SPEC §4.2): the
// packet and table support are separate because a packet default initializes
// storage, while a FORM-1 table default also governs elision and absent-field
// reads. The FIXED form elides nothing — a record is a constant-size body, so
// a declared default is the template's bytes — and this refusal does not
// apply to a default that lives only in tables the fixed form is emitted for.
// [refuseUnported] reaches the refusal for every port.
package compiler

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDefaultTargets is the canonical name of every built-in target whose
// backends carry a string, bytes or flags default on the FORM-1 table wire
// (elision and absent-field reads). Go registers from its target file's init.
// The fixed form is not this list: it elides nothing, and every leg that
// emits the form writes the default into the prefill.
var valueDefaultTargets = []string{"cpp", "c", "cs"}

// packetValueDefaultTargets names the packet carriers independently of the
// table carriers. A port registers here from its own target file's init.
var packetValueDefaultTargets = []string{"cpp"}

func registerPacketValueDefaultCarrier(name string) {
	packetValueDefaultTargets = append(packetValueDefaultTargets, name)
}

// refuseValueDefaults is the named refusal every target without the form
// gives a unit whose fields carry a string, bytes or flags default (SPEC
// §4.2): a port that has not met one would initialize the field to empty or
// zero where the schema says otherwise, and on the FORM-1 table wire would
// elide the wrong value. A default that lives only in a table
// [ir.TableFixedEmitted] names does not take this refusal.
func refuseValueDefaults(u *ir.Unit, target string) error {
	names := ir.ValueDefaultFields(u)
	if len(names) == 0 {
		return nil
	}
	// FORM-1 CLOSURE ONLY. A plain type can also supply table storage; follow
	// the same walk the table emitter uses (arrays, union arms, pointers, map
	// entries), but start from tables the fixed form is not emitted for. The
	// fixed form's defaults are the template, not elision.
	form1 := form1TableClosure(u)
	full := ir.TableClosure(u)
	var form1Names, packetNames []string
	for _, name := range names {
		owner, _, _ := strings.Cut(name, ".") // ValueDefaultFields returns Decl.field
		switch {
		case form1[owner]:
			form1Names = append(form1Names, name)
		case !full[owner]:
			packetNames = append(packetNames, name)
		}
	}
	if len(form1Names) > 0 && !slices.Contains(valueDefaultTargets, target) {
		carry, flags := carriers(valueDefaultTargets)
		return fmt.Errorf("unit puts a string, bytes or flags default in a table closure (%s): table-wire defaults are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
			englishList(form1Names), englishList(carry), target, englishList(flags))
	}
	if len(packetNames) == 0 || slices.Contains(packetValueDefaultTargets, target) {
		return nil
	}
	carry, flags := carriers(packetValueDefaultTargets)
	return fmt.Errorf("unit declares a string, bytes or flags default (%s): packet-wire defaults are %s only today, and the %s form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
		englishList(packetNames), englishList(carry), target, englishList(flags))
}

// form1TableClosure is [ir.TableClosure] reached from tables the FIXED FORM
// is not emitted for. Form 1 elides a field whose value is the declared
// default; a string, bytes or flags default in this closure still needs the
// form-1 carriers. A default that lives only under [ir.TableFixedEmitted]
// tables does not.
func form1TableClosure(u *ir.Unit) map[string]bool {
	closure := map[string]bool{}
	seenUnion := map[*ir.Union]bool{}
	var walk func(name string)
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
				walk(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	walk = func(name string) {
		if closure[name] {
			return
		}
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			return
		}
		closure[name] = true
		for _, f := range st.Fields {
			if f.IsMap() {
				walk(f.MapEntry.Name)
				continue
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				walk(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name, st := range u.Tables {
		if ir.TableFixedEmitted(u, st) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		walk(name)
	}
	return closure
}
