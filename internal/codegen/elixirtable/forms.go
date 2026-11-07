package elixirtable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func anyTableForm(u *ir.Unit, form string) bool {
	for _, st := range u.Tables {
		if st.CarriesForm(form) {
			return true
		}
	}
	return false
}

// A disabled root can still be storage inside an enabled parent. Keep that
// parent's closure descriptors and node accessors, without restoring root Open.
func cookFormClosure(u *ir.Unit) map[string]bool {
	declared := false
	for _, st := range u.Tables {
		declared = declared || st.DeclaredForms()
	}
	if !declared {
		return ir.TableClosure(u)
	}
	seen := map[string]bool{}
	seenUnion := map[*ir.Union]bool{}
	var walk func(*ir.Struct)
	var field func(*ir.Field)
	field = func(f *ir.Field) {
		if f.IsMap() {
			walk(f.MapEntry)
			return
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			walk(ref)
		case *ir.Union:
			if seenUnion[ref] {
				return
			}
			seenUnion[ref] = true
			for _, arm := range ref.Variants {
				if arm.F != nil {
					field(arm.F)
				}
			}
		}
	}
	walk = func(st *ir.Struct) {
		if st == nil || seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			field(f)
		}
	}
	g := gen{unit: u}
	for _, st := range u.Tables {
		if st.CarriesForm("cook") && g.cookable(st.Name) {
			walk(st)
		}
	}
	return seen
}

// The public Table API dispatches by root name. Disabled roots are refused at
// that boundary; private closure walks remain usable by an enabled parent.
func restrictFormRuntime(u *ir.Unit, names []string, runtime, cook string) (string, string, string) {
	var attrs strings.Builder
	for _, form := range []string{"json", "cook"} {
		if !anyTableForm(u, form) {
			if form == "json" {
				runtime = withoutJSONForm(runtime)
			} else {
				cook = ""
			}
			continue
		}
		var allowed []string
		restricted := false
		for _, name := range names {
			st := closureStruct(u, name)
			if !st.IsTable || st.CarriesForm(form) {
				allowed = append(allowed, elixirQuote(name))
			} else {
				restricted = true
			}
		}
		if !restricted {
			continue
		}
		attrs.WriteString(exAttribute(form+"_roots", "["+strings.Join(allowed, ", ")+"]"))
		attrs.WriteByte('\n')
		if form == "json" {
			runtime = strings.Replace(runtime, "  def to_json(table, value)", `  def to_json(table, _value) when table not in @json_roots,
    do: {:error, :form_not_generated}

  def to_json(table, value)`, 1)
			runtime = strings.Replace(runtime, "  def from_json(table, text)", `  def from_json(table, _text) when table not in @json_roots,
    do: {:error, :form_not_generated}

  def from_json(table, text)`, 1)
		} else {
			cook = strings.Replace(cook, "  def cook_measure(table, value)", `  def cook_measure(table, _value) when table not in @cook_roots, do: -1

  def cook_measure(table, value)`, 1)
			cook = strings.Replace(cook, "  def cook_measure_reason(table, value)", `  def cook_measure_reason(table, _value) when table not in @cook_roots,
    do: {-1, :form_not_generated}

  def cook_measure_reason(table, value)`, 1)
			cook = strings.Replace(cook, "  def cook_from(table, value, buffer, order)", `  def cook_from(table, _value, _buffer, _order) when table not in @cook_roots, do: false

  def cook_from(table, value, buffer, order)`, 1)
		}
	}
	return attrs.String(), runtime, cook
}

// JSON-only helpers are omitted with the unit's last JSON entry point. Shared
// wire report merging, framing, reference identity and extent measurement stay.
func withoutJSONForm(runtime string) string {
	extra := map[string]bool{}
	for _, name := range []string{"to_json", "to_json_identity", "from_json", "tally_field", "tally_node", "tally_refs", "fixed_text", "fixed_digits", "fixed_raw", "hex2", "hex_digit", "hexdigit", "indent", "clamp_json_int", "int_domain_bounds", "clamp_between", "timestamp_text", "timestamp_parse"} {
		extra[name] = true
	}
	return dropTopLevel(runtime, func(head string) bool {
		head = strings.TrimPrefix(strings.TrimPrefix(head, "  defp "), "  def ")
		name, _, ok := strings.Cut(head, "(")
		return ok && (strings.HasPrefix(name, "json_") || extra[name])
	})
}
