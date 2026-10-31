package elixir

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The typespec and documentation surface: @type t on every declaration
// module, @spec and @doc on every public function (SPEC §4.1, §6.1).

// typeSpec is a field's storage typespec.
func (g *gen) typeSpec(f *ir.Field) string {
	elem := g.scalarSpec(f)
	switch {
	case f.IsMap():
		return "map()"
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		elem = "[" + elem + "]"
	}
	if f.Type.Optional || f.Type.Pointer {
		return elem + " | nil"
	}
	return elem
}

// rangeSpec renders min..max as a typespec range.
func rangeSpec(min, max *big.Int) string {
	if min.Cmp(max) == 0 {
		return intLit(min) // a degenerate range is its one value
	}
	return intLit(min) + ".." + intLit(max)
}

// widthRange is the storage range of a bare integer of w bits.
func widthRange(w int, signed bool) string {
	if signed {
		half := new(big.Int).Lsh(big.NewInt(1), uint(w-1))
		return rangeSpec(new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1)))
	}
	return rangeSpec(big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(w)), big.NewInt(1)))
}

func (g *gen) scalarSpec(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return "boolean()"
	case ir.TFloat32, ir.TFloat64:
		return "float()"
	case ir.TString:
		return "String.t()"
	case ir.TBytes:
		return "binary()"
	case ir.TFixed:
		if f.IntMin != nil && f.IntMax != nil {
			lo, hi, _ := fixedRaw(f)
			return rangeSpec(lo, hi)
		}
		return "integer()"
	case ir.TBits:
		return widthRange(t.Width, false)
	case ir.TInt:
		if f.HasIntRange {
			return rangeSpec(f.IntMin, f.IntMax)
		}
		if t.Width > 0 {
			return widthRange(t.Width, t.Signed)
		}
		return "integer()"
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum, *ir.Union:
			return g.mod(t.Name) + ".t()"
		case *ir.Flags:
			return widthRange(64, false)
		case *ir.Struct:
			if ref.IsTable {
				return "struct()"
			}
			return g.mod(t.Name) + ".t()"
		}
	}
	return "term()"
}

// enumAtom is a variant's atom; None is :none.
func enumAtom(variant string) string { return ":" + elixirName(variant) }

// enumSpec is the @type t of an enum surface whose variants are named, with
// max the top wire value: the atoms, then the unnamed headroom past them as
// an integer range.
func enumSpec(variants []string, max int64) []string {
	alts := []string{":none"}
	for _, v := range variants {
		alts = append(alts, enumAtom(v))
	}
	if n := int64(len(variants)); max > n {
		if max == n+1 {
			alts = append(alts, intLit64(max))
		} else {
			alts = append(alts, intLit64(n+1)+".."+intLit64(max))
		}
	}
	return alts
}

// emitTypeAlts writes `@type t :: a | b | ...` in mix format's own shape: one
// line when it fits, otherwise one alternative per line under `@type t ::`.
func (g *gen) emitTypeAlts(alts []string) {
	one := "  @type t :: " + strings.Join(alts, " | ")
	if len(one) <= formatWidth {
		g.bpf("%s\n", one)
		return
	}
	g.bpf("  @type t ::\n")
	for i, a := range alts {
		if i == 0 {
			g.bpf("          %s\n", a)
			continue
		}
		g.bpf("          | %s\n", a)
	}
}

// emitStructType writes `@type t :: %__MODULE__{...}` in mix format's shape.
func (g *gen) emitStructType(fields []string) {
	if len(fields) == 0 {
		g.bpf("  @type t :: %%__MODULE__{}\n")
		return
	}
	one := "  @type t :: %__MODULE__{" + strings.Join(fields, ", ") + "}"
	if len(one) <= formatWidth {
		g.bpf("%s\n", one)
		return
	}
	g.bpf("  @type t :: %%__MODULE__{\n")
	for i, f := range fields {
		sep := ","
		if i == len(fields)-1 {
			sep = ""
		}
		if line := "          " + f + sep; len(line) > formatWidth {
			// the formatter breaks a field past the width after its key
			name, spec, _ := strings.Cut(f, ": ")
			g.bpf("          %s:\n            %s%s\n", name, spec, sep)
			continue
		}
		g.bpf("          %s%s\n", f, sep)
	}
	g.bpf("        }\n")
}

// emitModuleDoc writes a module's @moduledoc: the declaration's `///` block
// verbatim where it has one (SPEC §4.1), the generated description otherwise.
func (g *gen) emitModuleDoc(doc string, fallback ...string) {
	if doc != "" {
		g.bpf("  @moduledoc %s\n", ir.QuoteDocElixir(doc))
		return
	}
	g.emitHeredoc("@moduledoc", fallback)
}

// emitDoc writes a function's @doc heredoc from the given lines.
func (g *gen) emitDoc(lines ...string) {
	g.emitHeredoc("@doc", lines)
}

func (g *gen) emitHeredoc(attr string, lines []string) {
	if len(lines) == 1 && len("  "+attr+" "+fmt.Sprintf("%q", lines[0])) <= formatWidth && !strings.ContainsAny(lines[0], "\"\\#") {
		g.bpf("  %s \"%s\"\n", attr, lines[0])
		return
	}
	g.bpf("  %s \"\"\"\n", attr)
	for _, l := range lines {
		if l == "" {
			g.bpf("\n")
			continue
		}
		g.bpf("  %s\n", l)
	}
	g.bpf("  \"\"\"\n")
}

// emitSpec writes `@spec head :: ret`, broken after the `::` when the one
// line is past the formatter's width.
func (g *gen) emitSpec(head, ret string) {
	one := "  @spec " + head + " :: " + ret
	if len(one) <= formatWidth {
		g.bpf("%s\n", one)
		return
	}
	g.bpf("  @spec %s ::\n          %s\n", head, ret)
}

// EnumAtom is a variant's atom, the value of that variant in this target
// (the table backend spells its enum and union values with it too).
func EnumAtom(variant string) string { return enumAtom(variant) }

// EnumTerm is the Elixir value of an enum's wire ordinal: :none at 0, the
// variant's atom inside the declared variants, and the integer itself for an
// unnamed headroom value.
func EnumTerm(e *ir.Enum, ordinal int64) string {
	switch {
	case ordinal == 0:
		return ":none"
	case ordinal <= int64(len(e.Variants)):
		return enumAtom(e.Variants[ordinal-1])
	}
	return intLit64(ordinal)
}
