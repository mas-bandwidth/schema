package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// writeVariableFieldReadCanonical emits one table field's read, then the
// one-encoding refusal the field owes where it is first seen (docs/SPEC-TABLES.md
// §3, §3.1, §4; docs/rulings.json reads-refuse-non-canonical): a conforming
// writer elides a field that holds its declared default, so a field the reader
// names stored at that default is not the one encoding and the load refuses. The
// test mirrors the writer's own elision (writeFieldSave) kind by kind:
//
//   - a scalar, float (by its bits), enum, bool, string or byte buffer at its
//     default; a union at None;
//   - an empty list, map or counted array, and a fixed array whose every
//     element is at its default (a fixed array of tables always rides);
//   - a nested table whose body is only its terminator;
//   - an explicit null pointer, and an empty or all-null pointer array;
//   - a keyed array with no slot, or a slot at its default (writeKeyedFieldRead).
//
// A value that decodes to the default only because it names an id this reader
// cannot (an enum variant or a union arm from a newer writer) counts unknown and
// is lawful, so the test holds only when the field's read counted no unknown. A
// required field is always written and an optional one rides on its presence,
// so neither owes a test. Every lawful writer's default for a field this reader
// names is the reader's own (the lineage check, docs/VERSIONING.md), so the test
// does not depend on the writer's build version.
func writeVariableFieldReadCanonical(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field, retain bool, ordinal int) {
	ret := " return false;"
	if retain {
		ret = " return;"
	}
	if f.KeyEnum != "" && !f.Type.Optional {
		writeKeyedFieldRead(b, u, st, f, retain, ordinal, !f.Required)
		return
	}
	if f.Required || f.Type.Optional {
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		return
	}
	m := varMember(f)
	const ok = "!r.fail && !report.refused && !report.damaged"
	refuse := func(test string) {
		fmt.Fprintf(b, "                if (%s && (%s)) { report.refuse(\"non_canonical\");%s }\n", ok, test, ret)
	}
	switch {
	case f.Array == ir.ArrayNone && isStruct(f) && !f.Type.Pointer:
		// an empty nested body is one L byte of 1 and its terminator
		b.WriteString("                int canonAt = r.off;\n")
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		refuse("kind == 13 && r.off - canonAt == 2")
	case f.Array == ir.ArrayNone && f.Type.Pointer:
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		refuse(fmt.Sprintf("v.%sIdx == 0", m))
	case isPointerArray(f) && f.Array == ir.ArrayFixed:
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		fmt.Fprintf(b, "                { boolean canonRides = false; for (int i = 0; i < v.%sIdx.size(); i++) { if (v.%sIdx.get(i) != 0L) { canonRides = true; break; } }\n", m, m)
		refuse("!canonRides")
		b.WriteString("                }\n")
	case isPointerArray(f):
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		refuse(fmt.Sprintf("v.%sIdx.isEmpty()", m))
	case f.Type.Pointer || (f.Array == ir.ArrayFixed && isStruct(f)):
		// a fixed array of tables always rides (writeFieldSave)
		writeFieldRead(b, u, st, f, retain, ordinal, false)
	case f.Array == ir.ArrayFixed:
		one := variableElement(f)
		b.WriteString("                int canonUnk = report.unknown;\n")
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		fmt.Fprintf(b, "                { boolean canonRides = false; for (int i = 0; i < v.%s.size(); i++) { if (!(%s)) { canonRides = true; break; } }\n", m, variableDefaultTest(one, "v."+m+".get(i)"))
		refuse("report.unknown == canonUnk && !canonRides")
		b.WriteString("                }\n")
	default:
		b.WriteString("                int canonUnk = report.unknown;\n")
		writeFieldRead(b, u, st, f, retain, ordinal, false)
		refuse("report.unknown == canonUnk && " + variableDefaultTest(f, "v."+m))
	}
}
