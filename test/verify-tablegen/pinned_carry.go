package main

import (
	"fmt"
	"strings"
)

// THE CARRY OP 7 NEEDS WHERE A TARGET HAS TWO VALUE CLASSES. A fixed root's
// text form reads into the variable form's value (docs/SPEC-TABLES.md §16),
// while the message writer takes the fixed form's value, so on a target whose
// two are different classes op 7 carries the text-placed value across before
// it writes. Java does it by reflection in its driver; a target without
// reflection gets the pinned unit's carry written out here, from the one
// shape the unit holds (pinned.go). A unit that is not pinned gets none: no
// case of op 7 names it.

// pinnedElem is a pinned unit's element tag and whether its array is counted.
func pinnedElem(pkg string) (tag string, counted, ok bool) {
	for _, s := range pinnedShapes() {
		if "pin"+s.Tag == pkg {
			return s.Tag[1:], s.Tag[0] == 'c', true
		}
	}
	return "", false, false
}

// dartPinnedCarry fills `fv` (the fixed module's root) from `v` (the wire
// module's root) for the pinned unit pkg's fixed root, at ten spaces.
func dartPinnedCarry(pkg, root string) string {
	tag, counted, ok := pinnedElem(pkg)
	if !ok || root != pinnedFixRoot {
		return "          die(c.id, 'op 7: no carry for this root');\n"
	}
	wide := func(cls, x string) string {
		return fmt.Sprintf("%s_i.%s((%s >> 64).toSigned(64).toInt(), %s.toSigned(64).toInt())", pkg, cls, x, x)
	}
	var assign string
	switch tag {
	case "i128":
		assign = "fv.few[i] = " + wide("Int128", "v.few[i]") + ";"
	case "u128":
		assign = "fv.few[i] = " + wide("UInt128", "v.few[i]") + ";"
	case "rec":
		assign = "fv.few[i].n = v.few[i].n; fv.few[i].on = v.few[i].on;"
	case "wrec":
		assign = "fv.few[i].id = " + wide("UInt128", "v.few[i].id") + "; fv.few[i].mask = v.few[i].mask;"
	default:
		assign = "fv.few[i] = v.few[i];"
	}
	var b strings.Builder
	if counted {
		b.WriteString("          fv.fewCount = v.few.length;\n")
	}
	fmt.Fprintf(&b, "          for (var i = 0; i < v.few.length; i++) { %s }\n", assign)
	return b.String()
}
