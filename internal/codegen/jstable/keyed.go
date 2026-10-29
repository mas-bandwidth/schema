package jstable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE KEYED ACCESSORS (docs/SPEC-TABLES.md §2.4, docs/PORTING.md M5). An
// enum-keyed array [E]T has one slot per named variant: key k (E's variant
// value, 1..E.Max) lives at slot k-1. Every accessor takes the KEY and refuses
// None (0), any key past E.Max and anything that is not an integer, in every
// build, by a RangeError that names the field and the key. The language's own
// bound is no refusal here: a JavaScript Array read past its end answers
// undefined and a write there grows it. The entries yield the key beside the
// element, so the caller never does the shift.
//
// On a storage class the accessors are STATIC methods, `T.teamsAt(value, key)`,
// because a class's instance fields and its prototype share one namespace: a
// field spelled `teamsAt` would shadow an instance method. The static side
// holds only these and the built-ins (name, length, prototype), none of which
// ends in At or Entries.

// keyedGuard is the refusal, inline: one compare at each end and the integer
// check, then the throw.
func keyedGuard(where string, f *ir.Field) string {
	n := f.KeyEnumRef.Max
	return fmt.Sprintf("if (!Number.isInteger(key) || key < 1 || key > %d) { throw new RangeError(%s + String(key) + %s); }",
		n, jsStr(where+": key "), jsStr(fmt.Sprintf(" is not a %s variant (1..%d; None is 0 and has no slot)", f.KeyEnum, n)))
}

// emitKeyedStatics emits the three static accessors of one keyed field of a
// storage class whose instance property is prop.
func emitKeyedStatics(pf func(string, ...any), class, prop string, f *ir.Field) {
	up := strings.ToUpper(prop[:1]) + prop[1:]
	guard := keyedGuard(class+"."+prop, f)
	n := f.KeyEnumRef.Max
	pf("  // %s [%s]: key k (1..%d) at slot k-1; None and a key past %sMax are refused (§2.4)\n", prop, f.KeyEnum, n, f.KeyEnum)
	pf("  static %sAt(value, key) {\n    %s\n    return value.%s[key - 1];\n  }\n", prop, guard, prop)
	pf("  static set%sAt(value, key, element) {\n    %s\n    value.%s[key - 1] = element;\n  }\n", up, guard, prop)
	pf("  static *%sEntries(value) { for (let slot = 0; slot < %d; slot++) { yield [slot + 1, value.%s[slot]]; } }\n", prop, n, prop)
}
