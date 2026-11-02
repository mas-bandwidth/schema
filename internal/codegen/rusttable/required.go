package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

func hasRequired(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Required {
			return true
		}
	}
	return false
}

// One body's final guard values decide which unseen names are required.
// The successful path allocates nothing, even for bodies larger than 512 fields.
func emitRequiredNames(b *strings.Builder, st *ir.Struct) {
	if !hasRequired(st) {
		return
	}
	fmt.Fprintln(b, "#[allow(unused_variables)]")
	fmt.Fprintf(b, "pub fn %s(v: &%s, seen: &[bool]) -> String {\n", fn(st.Name, "required_missing"), st.Name)
	fmt.Fprintln(b, "    let mut missing = String::new();")
	conditions := guardConds(st, "v")
	for i, f := range st.Fields {
		if !f.Required {
			continue
		}
		cond := fmt.Sprintf("!seen[%d]", i)
		if guard := conditions[f.Name]; guard != "" {
			cond += " && (" + guard + ")"
		}
		fmt.Fprintf(b, "    if %s { if !missing.is_empty() { missing.push_str(\", \"); } missing.push_str(%q); }\n", cond, f.Name)
	}
	fmt.Fprint(b, "    missing\n}\n\n")
}

func emitRequiredJSONEnd(b *strings.Builder, st *ir.Struct, ind string) {
	if !hasRequired(st) {
		return
	}
	fmt.Fprintf(b, "%slet missing = %s(v, &seen);\n", ind, fn(st.Name, "required_missing"))
	fmt.Fprintf(b, "%sif !missing.is_empty() { jr.missing_required += 1; jr.missing_required_fields = missing; jr.reason = TableReason::MissingRequired; }\n", ind)
}
