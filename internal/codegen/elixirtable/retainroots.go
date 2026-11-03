package elixirtable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Retention belongs to variable roots (§6.6). Fixed bodies still participate
// beneath a variable root; only these public entry points reject fixed roots.
func restrictRetainRoots(u *ir.Unit, names []string, runtime string) (string, string) {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		// An all-fixed unit exposes the named refusals without the private reader,
		// writer, stores, or ID vocabulary used by a retained round trip.
		return "", fixedRetainRefusals
	}
	var fixed []string
	for _, name := range names {
		if !variable[name] {
			fixed = append(fixed, elixirQuote(name))
		}
	}
	if len(fixed) == 0 {
		return "", runtime
	}
	for _, change := range []struct{ entry, refusal string }{
		{"  def load_retain(table, data, retain, report)", `  def load_retain(table, _data, retain, report) when table in @retain_fixed_roots,
    do: {:error, :fixed_root, report, retain}

`},
		{"  def measure_retain(table, value, retain)", `  def measure_retain(table, _value, _retain) when table in @retain_fixed_roots,
    do: {:error, :fixed_root}

`},
		{"  def save_retain(table, value, retain, report)", `  def save_retain(table, _value, retain, report) when table in @retain_fixed_roots,
    do: {:error, :fixed_root, report, retain}

`},
	} {
		if strings.Count(runtime, change.entry) != 1 {
			panic("elixirtable: retaining entry point not found exactly once")
		}
		runtime = strings.Replace(runtime, change.entry, change.refusal+change.entry, 1)
	}
	return exAttribute("retain_fixed_roots", "["+strings.Join(fixed, ", ")+"]") + "\n", runtime
}

const fixedRetainRefusals = `
  # Retention requires a variable root (docs/SPEC-TABLES.md §6.6).
  # Refuse before inspecting input or changing the caller's report and stores.
  def load_retain(_table, _data, retain, report), do: {:error, :fixed_root, report, retain}

  def measure_retain(_table, _value, _retain), do: {:error, :fixed_root}

  def save_retain(_table, _value, retain, report), do: {:error, :fixed_root, report, retain}
`
