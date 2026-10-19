package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableFormEmitsFormatCanonicalSource pins issue #547's last half on
// the Makefile side. The variable form's Table.ex and TableRuntime.ex are
// emitted in mix format's shape, so make/elixir.mk's raw-format gate holds
// every file the table backend writes: ELIXIR_EMIT_FORMAT_PENDING is set and
// EMPTY. And no stamp or generate recipe runs `$(MIX) format ` without
// --check-formatted over generated table units, so tables-elixir-clean and
// the stamps that compile those units see the emitters' raw output and not a
// canonicalized copy of it. It reads make/elixir.mk only: no Elixir, no clock.
func TestVariableFormEmitsFormatCanonicalSource(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")

	found := false
	for _, line := range lines {
		if name, value, ok := strings.Cut(line, ":="); ok && strings.TrimSpace(name) == "ELIXIR_EMIT_FORMAT_PENDING" {
			found = true
			if p := strings.Fields(value); len(p) != 0 {
				t.Errorf("ELIXIR_EMIT_FORMAT_PENDING names %v: the raw-format gate must hold every emitted file", p)
			}
		}
	}
	if !found {
		t.Fatal("make/elixir.mk sets no ELIXIR_EMIT_FORMAT_PENDING")
	}

	// every recipe line, with the target it belongs to; a backslash
	// continuation is part of the line it continues
	target := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "\t") {
			if head, _, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, "#") && !strings.Contains(head, "=") && !strings.HasPrefix(line, " ") {
				target = strings.TrimSpace(head)
			}
			continue
		}
		r := line
		for strings.HasSuffix(strings.TrimSpace(lines[i]), "\\") && i+1 < len(lines) {
			i++
			r += "\n" + lines[i]
		}
		if strings.HasPrefix(strings.TrimSpace(r), "#") {
			continue
		}
		if !strings.Contains(r, "$(MIX) format ") || strings.Contains(r, "--check-formatted") {
			continue
		}
		if strings.Contains(r, "tables-generated-elixir") || strings.Contains(r, "$$files") ||
			strings.Contains(r, "elixir-fixedform") || strings.Contains(r, "elixir-fixed-bench") ||
			strings.Contains(r, "generated/bench/paired/elixir") || strings.Contains(r, "ELIXIR_EMIT_FORMAT") {
			t.Errorf("make/elixir.mk %s runs mix format without --check-formatted over generated table units: %q", target, strings.TrimSpace(r))
		}
	}
}

func TestCookExtentSourceMatchesNestedDescriptorCapability(t *testing.T) {
	t.Parallel()
	fixed := unitFrom(t, `package probe
fixed table Root { value int32 }
`)
	fixedFiles := generateVariableForm(fixed, "Probe")
	fixedSource := string(fixedFiles[VariableTableModule+".ex"])
	for _, forbidden := range []string{"cook_extent(", "cook_carve(", "cook_extent_field(", "cook_extent_after(", "cook_carve_field(", "cook_carve_after(", "n.desc.has_extent", "@dialyzer"} {
		if strings.Contains(fixedSource, forbidden) {
			t.Errorf("fixed-only source still contains %q", forbidden)
		}
	}

	nestedExtent := unitFrom(t, `package probe
 table Child { values []uint8 }
 table Root { child Child }
`)
	nestedFiles := generateVariableForm(nestedExtent, "Probe")
	nestedSource := string(nestedFiles[VariableTableModule+".ex"])
	for _, required := range []string{"has_extent: true", "cook_extent(", "cook_carve("} {
		if !strings.Contains(nestedSource, required) {
			t.Errorf("nested extent source lacks %q", required)
		}
	}
}

func TestVariableRuntimeUnionHelpersMatchIR(t *testing.T) {
	t.Parallel()
	noUnion := unitFrom(t, `package probe
fixed table Root { value int32 }
`)
	noUnionSource := string(generateVariableForm(noUnion, "Probe")[VariableTableModule+".ex"])
	for _, forbidden := range []string{"defp collect_union_nodes(", "collect_union_nodes(", "defp arm_header(", "arm_header(", "defp read_arm(", "read_arm(", "defp json_arm(", "json_arm(", "defp json_read_arm(", "json_read_arm(", ":union ->", "@dialyzer"} {
		if strings.Contains(noUnionSource, forbidden) {
			t.Errorf("no-union output contains %q", forbidden)
		}
	}

	withUnion := unitFrom(t, `package probe
union Pick { value int32 }
table Root { pick Pick }
`)
	withUnionSource := string(generateVariableForm(withUnion, "Probe")[VariableTableModule+".ex"])
	for _, required := range []string{"defp collect_union_nodes(", "defp arm_header(", "defp read_arm(", "defp json_arm(", "defp json_read_arm("} {
		if !strings.Contains(withUnionSource, required) {
			t.Errorf("union output lacks %q", required)
		}
	}
	if strings.Contains(withUnionSource, "defp read_arm_payload(%{t: :void}") {
		t.Fatal("non-void union output retained the void payload clause")
	}
	if strings.Contains(withUnionSource, "defp read_list_elements(%{elem_kind: 15}") {
		t.Fatal("union output without a list-of-union retained its list reader clause")
	}
	withUnionList := unitFrom(t, `package probe
union Pick { value int32 }
table Root { picks []Pick }
`)
	withUnionListSource := string(generateVariableForm(withUnionList, "Probe")[VariableTableModule+".ex"])
	for _, required := range []string{"defp read_list_elements(%{elem_kind: 15}", "defp json_list_elem(%{elem_kind: 15}"} {
		if !strings.Contains(withUnionListSource, required) {
			t.Errorf("list-of-union output lacks %q", required)
		}
	}

	withVoid := unitFrom(t, `package probe
union Pick { empty }
table Root { pick Pick }
`)
	withVoidSource := string(generateVariableForm(withVoid, "Probe")[VariableTableModule+".ex"])
	for _, required := range []string{"defp read_arm_payload(%{t: :void}", "defp arm_content(%{t: :void}"} {
		if !strings.Contains(withVoidSource, required) {
			t.Errorf("void-arm output lacks %q", required)
		}
	}
	if strings.Contains(withVoidSource, "@dialyzer") {
		t.Fatal("union output must not rely on Dialyzer suppression")
	}
}

func TestVariableRuntimeKeyedTableElementBranchMatchesIR(t *testing.T) {
	t.Parallel()
	scalar := unitFrom(t, `package probe
        enum Slot { A, B }
        table Root { slots [Slot]int32 }
        `)
	scalarSource := string(generateVariableForm(scalar, "Probe")[VariableTableModule+".ex"])
	if strings.Contains(scalarSource, "if f.elem_kind == 13") {
		t.Fatal("scalar keyed element output retained the table-element reader branch")
	}
	if strings.Contains(scalarSource, "@dialyzer") {
		t.Fatal("scalar keyed element output used a Dialyzer exemption")
	}

	tableValue := unitFrom(t, `package probe
        enum Slot { A, B }
        table Item { value int32 }
        table Root { slots [Slot]Item }
        `)
	tableSource := string(generateVariableForm(tableValue, "Probe")[VariableTableModule+".ex"])
	if !strings.Contains(tableSource, "if f.elem_kind == 13") {
		t.Fatal("table-valued keyed output omitted the reachable element reader branch")
	}
	if strings.Contains(tableSource, "@dialyzer") {
		t.Fatal("table-valued keyed output used a Dialyzer exemption")
	}
}

func TestVariableRuntimeUnionCapabilityMatrix(t *testing.T) {
	t.Parallel()
	mixed := unitFrom(t, `package probe
        enum Grade { First, Second }
        type Leaf { value int32 }
        union Inner { leaf Leaf }
        union Mixed {
          empty
          integer int32
          real float32
          flag bool
          text string(8)
          wide wstring(4)
          raw bytes(4)
          grade Grade
          nested Inner
        }
        fixed table Root { choice Mixed }
        `)
	source := string(generateVariableForm(mixed, "Probe")[VariableTableModule+".ex"])
	for _, required := range []string{
		"defp collect_union_nodes(", "defp arm_header(", "defp read_arm(",
		"defp arm_content(%{t: :void}", "defp arm_content(%{t: :table}",
		"defp arm_content(%{t: :int}", "defp arm_content(%{t: :float}",
		"defp arm_content(%{t: :bool}", "defp arm_content(%{t: :string}",
		"defp arm_content(%{t: :wstring}", "defp arm_content(%{t: :bytes}",
		"defp arm_content(%{t: :enum}", "defp arm_content(%{t: t}, nil",
		"defp arm_content(%{t: :list}", "defp arm_content(%{t: :union}",
		"defp read_arm_payload(%{t: :void}", "defp json_arm(", "defp json_read_arm(",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("mixed union output lost reachable arm capability %q", required)
		}
	}
	if strings.Contains(source, "@dialyzer") {
		t.Fatal("mixed union output used a Dialyzer exemption")
	}
}

func TestCookExtentSourceKeepsDirectAndListOfTablePaths(t *testing.T) {
	t.Parallel()
	direct := unitFrom(t, `package probe
        table Root {
          text string(8)
          values []uint8
        }
        `)
	directSource := string(generateVariableForm(direct, "Probe")[VariableTableModule+".ex"])
	for _, required := range []string{"cook_extent_field(", "cook_carve_field("} {
		if !strings.Contains(directSource, required) {
			t.Errorf("direct extent output lacks %q", required)
		}
	}

	listOfTables := unitFrom(t, `package probe
        table Child { values []uint8 }
        table Root { children []Child }
        `)
	listSource := string(generateVariableForm(listOfTables, "Probe")[VariableTableModule+".ex"])
	for _, required := range []string{"cook_extent_after(%{elem_kind: 13", "cook_carve_after(", "cook_extent_field(", "cook_carve_field("} {
		if !strings.Contains(listSource, required) {
			t.Errorf("list-of-extent-bearing-tables output lacks %q", required)
		}
	}
}
