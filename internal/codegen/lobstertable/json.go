// The JSON text form (docs/SPEC-TABLES.md §16 through §16.13;
// docs/PORT-LOBSTER.md, THE CARD MAP): runtime/json.lobster carries the one
// read walk and the one write walk over the descriptors, and this part joins
// every table module to it through Register.
package lobstertable

import (
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("json", emitJson) }

// emitJson imports the JSON runtime and registers the facts the descriptors
// do not carry, from the IR: a flags field's (or arm's) variant names, a text
// list field (its JSON is a plain array of strings, §2.13), a `json = "array"`
// table (§16.10) and a guarded field's branch condition (the write's
// elision, Go's tabletext.Guards, walked here
// by jsonGuards so the generator imports no engine package).
func emitJson(g *lobster.Gen) error {
	g.Import("json")
	key := func(owner, field string) string {
		return lobster.StringLit([]byte(owner)) + ", " + lobster.StringLit([]byte(field))
	}
	field := func(owner string, f *ir.Field) {
		if fl, ok := f.Type.Ref.(*ir.Flags); ok && f.Type.Kind == ir.TNamed {
			vs := make([]string, len(fl.Variants))
			for i, v := range fl.Variants {
				vs[i] = lobster.StringLit([]byte(v))
			}
			g.Pf("schema_rt.json_flags(%s, [%s]::string)\n", key(owner, f.Name), strings.Join(vs, ", "))
		}
		if f.IsTextList() {
			g.Pf("schema_rt.json_text_list(%s)\n", key(owner, f.Name))
		}
	}
	seen := map[string]bool{}
	structure := func(st *ir.Struct) {
		if st == nil || seen[st.Name] {
			return
		}
		seen[st.Name] = true
		if st.JsonArray {
			g.Pf("schema_rt.json_array_table(%s)\n", lobster.StringLit([]byte(st.Name)))
		}
		for _, f := range st.Fields {
			field(st.Name, f)
		}
		guards := jsonGuards(st)
		names := make([]string, 0, len(guards))
		for n := range guards {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			var conds, wants []string
			for _, t := range guards[n] {
				conds = append(conds, lobster.StringLit([]byte(t.Field)))
				if t.Want {
					wants = append(wants, "true")
				} else {
					wants = append(wants, "false")
				}
			}
			g.Pf("schema_rt.json_guard(%s, [%s]::string, [%s]::bool)\n", key(st.Name, n), strings.Join(conds, ", "), strings.Join(wants, ", "))
		}
	}
	for _, t := range g.File.Tables {
		structure(t)
	}
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			structure(st)
		}
	}
	for _, un := range g.File.TableUnions {
		for _, v := range un.Variants {
			if v.F != nil {
				f := *v.F
				f.Name = v.Name
				field(un.Name, &f)
			}
		}
	}
	return nil
}

// jsonGuard is one term of a guarded field's condition: the bool field it
// reads and the value it must hold.
type jsonGuard struct {
	Field string
	Want  bool
}

// jsonGuards maps each guarded field of st to its condition, outermost term
// first: the same walk as the Go engine's tabletext.Guards, kept here so the
// generator never imports the engine it is judged against (internal/ci,
// TestSchemaEngineIsTheOracleImportIndependence).
func jsonGuards(st *ir.Struct) map[string][]jsonGuard {
	out := map[string][]jsonGuard{}
	var walk func(items []ir.Item, cond []jsonGuard)
	walk = func(items []ir.Item, cond []jsonGuard) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					out[item.F.Name] = append([]jsonGuard(nil), cond...)
				}
			case *ir.Branch:
				walk(item.Then, append(cond[:len(cond):len(cond)], jsonGuard{item.Cond, !item.Neg}))
				walk(item.Else, append(cond[:len(cond):len(cond)], jsonGuard{item.Cond, item.Neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}
