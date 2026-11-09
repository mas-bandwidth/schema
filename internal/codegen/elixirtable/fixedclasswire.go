package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// withFixedClassWire makes a declared fixed root's table-wire load and save
// the fixed form (docs/SPEC-TABLES.md §3, §3.4). Form 3 is that root's file.
// Form 1 is previous_form and nothing is decoded. A read returns the one
// record — the whole value — or a refusal, never a prefix of the batch.
// Variable roots stay on the id-table codec below these clauses.
func withFixedClassWire(u *ir.Unit, ns, runtime string) string {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return runtime
	}
	var save, load strings.Builder
	save.WriteString("  # A declared fixed root's file is form 3 (docs/SPEC-TABLES.md §3, §3.4).\n")
	save.WriteString("  # The write is the fixed form's own. The variable codec below is for\n")
	save.WriteString("  # variable roots, including one that holds a fixed table.\n\n")
	load.WriteString("  # A declared fixed root is read as form 3, one record or a refusal.\n")
	load.WriteString("  # Form 1 is previous_form: the form byte is named and nothing is decoded.\n\n")
	for _, st := range roots {
		mod := fmt.Sprintf("%s.%s%s", ns, moduleBase(u.DeclFile[st.Name]), FixedModuleSuffix)
		snake := ir.RustSnake(st.Name)
		save.WriteString(fixedRootSaveClause(st.Name, mod+"."+snake+"_fixed_save([value])"))
		load.WriteString(fixedRootLoadClauses(st.Name, mod+"."+snake+"_fixed_load(data)"))
	}
	runtime = insertOnce(runtime, "  # ---- the write side (docs/SPEC-TABLES.md §3) ----\n", save.String())
	runtime = insertOnce(runtime, "  # ---- the read side (docs/SPEC-TABLES.md §3, §4) ----\n", load.String())
	return runtime
}

func insertOnce(src, anchor, clause string) string {
	if strings.Count(src, anchor) != 1 {
		panic("elixirtable: fixed-class wire anchor not found exactly once: " + anchor)
	}
	return strings.Replace(src, anchor, clause+anchor, 1)
}

// The heads and calls below are what Elixir 1.20.4's mix format (line length
// 98) keeps. A case head stays intact through 101 columns and breaks at 102.
// A save assignment breaks at `=` past 98, and a broken assignment keeps a
// blank line before the next statement.
func fixedRootSaveClause(name, call string) string {
	var b strings.Builder
	b.WriteString(exSaveHead(name))
	one := "    data = " + call
	if exWidth(one) <= exLineLimit {
		b.WriteString(one + "\n")
	} else {
		b.WriteString("    data =\n")
		b.WriteString(exBreakSaveCall(call))
		b.WriteString("\n")
	}
	b.WriteString("    {:ok, IO.iodata_to_binary(data)}\n")
	b.WriteString("  end\n\n")
	return b.String()
}

func exBreakSaveCall(call string) string {
	if cont := "      " + call; exWidth(cont) <= exLineLimit {
		return cont + "\n"
	}
	head := strings.TrimSuffix(call, "([value])")
	if bracket := "      " + head + "(["; exWidth(bracket) <= exLineLimit {
		return bracket + "\n        value\n      ])\n"
	}
	return "      " + head + "(\n        [value]\n      )\n"
}

func fixedRootLoadClauses(name, call string) string {
	var b strings.Builder
	b.WriteString(exLoadFormHead(name))
	b.WriteString("    report = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, layout_hash: 0}\n")
	b.WriteString("    {:error, :previous_form, report}\n")
	b.WriteString("  end\n\n")
	b.WriteString(exLoadDataHead(name))
	b.WriteString(exLoadCase(call))
	b.WriteString("      {:ok, [value], report} -> {:ok, value, report}\n")
	b.WriteString("      {:ok, [], report} -> {:error, :truncated, report}\n")
	b.WriteString("      {:error, reason, report} -> {:error, reason, report}\n")
	b.WriteString("      {:ok, _values, report} -> {:error, :batch_too_large, report}\n")
	b.WriteString("    end\n")
	b.WriteString("  end\n\n")
	return b.String()
}

func exLoadCase(call string) string {
	one := "    case " + call + " do"
	// Elixir 1.20.4 keeps this head through 101 columns.
	if exWidth(one) <= exLineLimit+3 {
		return one + "\n"
	}
	head := strings.TrimSuffix(call, "(data)")
	return "    case " + head + "(\n           data\n         ) do\n"
}

func exSaveHead(name string) string {
	one := fmt.Sprintf("  def save(%q, value) when is_map(value) do", name)
	if exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	short := fmt.Sprintf("  def save(%q, value)", name)
	if exWidth(short) <= exLineLimit {
		return short + "\n      when is_map(value) do\n"
	}
	return fmt.Sprintf("  def save(\n        %q,\n        value\n      )\n      when is_map(value) do\n", name)
}

func exLoadFormHead(name string) string {
	one := fmt.Sprintf("  def load(%q, <<1, _::binary>>) do", name)
	if exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	return fmt.Sprintf("  def load(\n        %q,\n        <<1, _::binary>>\n      ) do\n", name)
}

func exLoadDataHead(name string) string {
	one := fmt.Sprintf("  def load(%q, data) when is_binary(data) do", name)
	if exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	short := fmt.Sprintf("  def load(%q, data)", name)
	if exWidth(short) <= exLineLimit {
		return short + "\n      when is_binary(data) do\n"
	}
	return fmt.Sprintf("  def load(\n        %q,\n        data\n      )\n      when is_binary(data) do\n", name)
}
