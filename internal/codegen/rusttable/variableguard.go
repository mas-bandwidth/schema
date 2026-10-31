package rusttable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A FIELD THE WRITER DOES NOT WRITE IS NOT AN EDGE (docs/SPEC-TABLES.md §3.1,
// §4): a field inside an `if` group rides only while its branch condition
// holds, so the save, the numbering walk and the text writer's two passes all
// skip it on the same test, and a pointer under a false guard takes no node
// index and writes no record. The read places every field the wire carries,
// the oracle's decode exactly (internal/tablewire): the guard is a plain bool
// field, and nothing is inferred from it on the way in.

// guardConds maps each guarded field of one body to its Rust condition over
// the body's own bool fields, reached through recv ("v" for `v.at_rest`): the
// conjunction of every enclosing branch of the `if` tree (st.Items), negated
// under `if !x` and under an `else`, composed through nesting exactly as the
// oracle composes it (internal/tabletext Guards). A field with no entry always
// rides.
func guardConds(st *ir.Struct, recv string) map[string]string {
	out := map[string]string{}
	var walk func(items []ir.Item, cond []string)
	walk = func(items []ir.Item, cond []string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					out[item.F.Name] = strings.Join(cond, " && ")
				}
			case *ir.Branch:
				pos, neg := recv+"."+ir.RustSnake(item.Cond), "!"+recv+"."+ir.RustSnake(item.Cond)
				if item.Neg {
					pos, neg = neg, pos
				}
				walk(item.Then, append(cond[:len(cond):len(cond)], pos))
				walk(item.Else, append(cond[:len(cond):len(cond)], neg))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

// guardedField emits one field's code, which emit writes at the function
// body's indent, under its guard (cond, "" for an unguarded field). A field
// whose code is ONE `if` with no `else` takes the guard as the first operand
// of that `if`'s own condition, the let-chain clippy's collapsible_if asks
// for (`if v.at_rest && let Some(n) = &v.resting { .. }`); any other shape
// is wrapped whole in `if cond { .. }`.
func guardedField(b *strings.Builder, cond string, emit func(b *strings.Builder)) {
	var inner strings.Builder
	emit(&inner)
	code := inner.String()
	if cond == "" || code == "" {
		// a field the walk has nothing to do for takes no empty `if` either
		b.WriteString(code)
		return
	}
	if soleIf(code) {
		b.WriteString(bodyIndent + "if " + cond + " && " + strings.TrimPrefix(code, bodyIndent+"if "))
		return
	}
	b.WriteString(bodyIndent + "if " + cond + " {\n")
	for line := range strings.SplitAfterSeq(code, "\n") {
		if line != "" && line != "\n" {
			b.WriteString("    ")
		}
		b.WriteString(line)
	}
	b.WriteString(bodyIndent + "}\n")
}

// bodyIndent is a generated function body's own indent.
const bodyIndent = "    "

// soleIf reports whether code, written at bodyIndent, is exactly one `if`
// statement with no `else` and a condition that takes `cond &&` in front of
// it unchanged: one line `if .. { .. }`, or a first line `if .. {`, lines
// indented deeper, and a last line `}` at bodyIndent.
func soleIf(code string) bool {
	if !strings.HasPrefix(code, bodyIndent+"if ") || !strings.HasSuffix(code, "}\n") {
		return false
	}
	lines := strings.Split(strings.TrimSuffix(code, "\n"), "\n")
	head := lines[0]
	if strings.Contains(head, "else") || strings.Contains(head[:strings.LastIndex(head, "{")+1], "||") {
		return false
	}
	if len(lines) == 1 {
		return true
	}
	if !strings.HasSuffix(head, "{") || lines[len(lines)-1] != bodyIndent+"}" {
		return false
	}
	for _, line := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(line, bodyIndent+" ") {
			return false
		}
	}
	return true
}
