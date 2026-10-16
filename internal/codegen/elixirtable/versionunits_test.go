package elixirtable_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// versionUnits are the manifest units this test answers for Elixir, by the
// manifest's own key (testdata/conformance/tables/MANIFEST.txt `unit` lines):
// the guard unit, the enum-kind pair and the later scalars reader, and the two
// vocabulary units whose plain wire instances ride the same codec.
var versionUnits = []string{"tblg1", "tblk1", "tblk2", "tblscalars2", "vocabdemo", "vocab9demo"}

// versionUnitAbsences are the rows this leg answers ABSENT, each with its
// reason. A row named here must still be refused for that reason: when the
// codec starts carrying it, this test fails by name so the absence is dropped
// rather than left standing over a row that now has an answer.
var versionUnitAbsences = map[string]string{
	// SimState holds an enum-keyed array (axes [Axis]) and the fixed-point
	// and 128-bit kinds (18-29), which this codec does not carry at this base:
	// uncarriedRootNames names it, so load answers {:error, :uncarried}. The
	// scalars unit's card (elixir-carries-maps-and-union-arms-on-the-table-wire-2)
	// carries those constructs; these three rows are answered once it lands.
	"scalars_full_as_2":  "uncarried",
	"scalars_edges_as_2": "uncarried",
	"field_scalar_widen": "uncarried",
}

// TestGuardKindAndScalarVersionUnitsElixir pins docs/SPEC-TABLES.md §3.1 and
// §4 for the Elixir leg over the guard (G1), enum-kind (K1/K2), second-scalars
// (Scalars2) and vocabulary units of the conformance manifest.
//
// The accept clause is a unit test: each unit generates for Elixir through the
// compiler path (`schema generate --lang elixir`) and names a Table module, and
// G1's guarded pointer carries its branch in the descriptor so the save walk
// can skip it (schema#440, #526).
//
// The rest is functional and runs elixir: under SCHEMA_REQUIRE_CORPUS=1 (it
// skips by name without the toolchain and fails by name with it), every
// instance row of these units loads through the generated Table module and
// re-saves to the Go engine's committed bytes, and every report row's counters
// equal the Go engine's reports.txt row, or the row is a named absence above.
// A false guard and an absent optional are not edges: the guard-true value
// with its guard turned off saves the guard-false bytes, and the present
// optional dropped saves the absent-optional bytes.
func TestGuardKindAndScalarVersionUnitsElixir(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifest := readVersionManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	reports := readVersionReports(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))

	type generated struct {
		mod   string
		files map[string][]byte
	}
	gen := map[string]generated{}
	c := compiler.New()
	for _, key := range versionUnits {
		path, ok := manifest.units[key]
		if !ok {
			t.Fatalf("%s: the manifest names no unit by this key", key)
		}
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, path)})
		if err != nil {
			t.Fatalf("%s: gather %s: %v", key, path, err)
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s: load %s: %v", key, path, err)
		}
		files, err := c.Generate(u, "elixir", compiler.Options{})
		if err != nil {
			t.Fatalf("%s: %s refused for elixir: %v", key, path, err)
		}
		if _, ok := files["Table.ex"]; !ok {
			t.Fatalf("%s: generated no Table.ex, so the driver finds no codec and every row is absent", key)
		}
		gen[key] = generated{mod: ir.GoExportName(u.Package) + ".Table", files: files}
	}

	// G1's guarded pointer names its branch: the walk skips it when at_rest
	// is false, so a save numbers no node no written field names.
	g1 := string(gen["tblg1"].files["Table.ex"])
	if !strings.Contains(g1, `guard: [{"at_rest", true}]`) {
		t.Errorf("tblg1: the resting pointer's descriptor carries no guard; the save walk cannot skip it under a false guard")
	}

	elixirBin := versionElixir(t)

	for _, key := range versionUnits {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			g := gen[key]
			dir := t.TempDir()
			names := writeVersionModules(t, dir, g.files)

			var rows []versionRow
			for _, r := range manifest.rows {
				if r.unit == key {
					rows = append(rows, r)
				}
			}
			if len(rows) == 0 {
				t.Fatalf("%s: the manifest names no instance or report row for this unit", key)
			}

			var probe strings.Builder
			probe.WriteString("mod = " + g.mod + "\n")
			probe.WriteString(versionProbeHelpers)
			probe.WriteString("rows = [\n")
			for _, r := range rows {
				fmt.Fprintf(&probe, "  {%q, %q, %q, %q},\n", r.kind, r.name, r.root, filepath.Join(root, r.wire))
			}
			probe.WriteString("]\n")
			probe.WriteString(versionProbeRows)
			if key == "tblg1" {
				w := func(n string) string {
					return fmt.Sprintf("%q", filepath.Join(root, "testdata", "wire", "tables", n+".bin"))
				}
				probe.WriteString(`
{:ok, both, _} = mod.load("Guarded", File.read!(` + w("guard_true_both_edges") + `))
{:ok, off} = mod.save("Guarded", Map.put(both, "at_rest", false))
IO.puts("ROW guard_turned_off " <> if(off == File.read!(` + w("guard_false_no_edge") + `), do: "same", else: "differs " <> Base.encode16(off, case: :lower)))
{:ok, present, _} = mod.load("Guarded", File.read!(` + w("optional_present_edge") + `))
{:ok, dropped} = mod.save("Guarded", Map.delete(present, "optional"))
IO.puts("ROW optional_dropped " <> if(dropped == File.read!(` + w("optional_absent_no_edge") + `), do: "same", else: "differs " <> Base.encode16(dropped, case: :lower)))
`)
				rows = append(rows, versionRow{kind: "wire", name: "guard_turned_off"}, versionRow{kind: "wire", name: "optional_dropped"})
			}

			out := runVersionProbe(t, elixirBin, dir, names, probe.String())
			got := map[string]string{}
			for _, line := range strings.Split(out, "\n") {
				if rest, ok := strings.CutPrefix(line, "ROW "); ok {
					name, answer, _ := strings.Cut(rest, " ")
					got[name] = answer
				}
			}
			for _, r := range rows {
				answer, ok := got[r.name]
				if !ok {
					t.Errorf("%s %s: the probe gave no answer", r.kind, r.name)
					continue
				}
				if reason, absent := versionUnitAbsences[r.name]; absent {
					if answer != reason {
						t.Errorf("%s %s: named absent (%s) but answered %q: drop the absence and check the row", r.kind, r.name, reason, answer)
					}
					continue
				}
				switch r.kind {
				case "wire":
					if answer != "same" {
						t.Errorf("wire %s: load then save does not reproduce the Go engine's bytes: %s", r.name, answer)
					}
				case "report":
					want, ok := reports[r.name]
					if !ok {
						t.Errorf("report %s: reports.txt has no row", r.name)
					} else if answer != want {
						t.Errorf("report %s: counters %q, the Go engine's %q", r.name, answer, want)
					}
				}
			}
		})
	}
}

// versionProbeHelpers answers one row: an instance loads and re-saves, a
// report renders the driver's counter line (test/conformance/elixir/driver_impl.ex).
const versionProbeHelpers = `
defmodule VersionProbe do
  def counters(r) do
    [Map.get(r, :unknown, 0), Map.get(r, :kind_mismatch, 0), Map.get(r, :widened, 0),
     Map.get(r, :clamped, 0), 0, if(Map.get(r, :malformed, false), do: "true", else: "false")]
    |> Enum.join(",")
  end

  def answer(mod, "wire", root, wire) do
    case mod.load(root, wire) do
      {:ok, value, _report} ->
        case mod.save(root, value) do
          {:ok, ^wire} -> "same"
          {:ok, out} -> "differs " <> Base.encode16(out, case: :lower)
          other -> "save " <> inspect(other)
        end

      {:error, :uncarried} -> "uncarried"
      other -> "load " <> inspect(other)
    end
  end

  def answer(mod, "report", root, wire) do
    case mod.load(root, wire) do
      {:ok, _value, report} -> counters(report) <> ",read"
      {:error, :newer_form, _} -> "0,0,0,0,0,false,refused"
      {:error, :malformed, _} -> "0,0,0,0,0,true,read"
      {:error, :uncarried} -> "uncarried"
      other -> "load " <> inspect(other)
    end
  end
end
`

const versionProbeRows = `
for {kind, name, root, path} <- rows do
  IO.puts("ROW " <> name <> " " <> VersionProbe.answer(mod, kind, root, File.read!(path)))
end
`

type versionRow struct {
	kind, name, unit, root, wire string
}

type versionManifest struct {
	units map[string]string
	rows  []versionRow
}

// readVersionManifest reads the unit, instance and report lines.
func readVersionManifest(t *testing.T, path string) versionManifest {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := versionManifest{units: map[string]string{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch {
		case fields[0] == "unit" && len(fields) >= 3:
			m.units[fields[1]] = fields[2]
		case fields[0] == "instance" && len(fields) >= 5:
			m.rows = append(m.rows, versionRow{kind: "wire", name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]})
		case fields[0] == "report" && len(fields) >= 5:
			m.rows = append(m.rows, versionRow{kind: "report", name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

// readVersionReports reads the Go engine's report rows, keyed by case.
func readVersionReports(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && !strings.HasPrefix(fields[0], "#") {
			out[fields[0]] = fields[1]
		}
	}
	return out
}

// versionElixir finds the leg's elixir: ELIXIR, the repo-local dist/, or PATH.
// Without one it skips by name, unless SCHEMA_REQUIRE_CORPUS is set, where it
// fails by name.
func versionElixir(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("ELIXIR"); env != "" {
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
	}
	if bin, err := filepath.Abs("../../../dist/elixir-1.20.4/bin/elixir"); err == nil {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	if bin, err := exec.LookPath("elixir"); err == nil {
		return bin
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("TestGuardKindAndScalarVersionUnitsElixir: SCHEMA_REQUIRE_CORPUS is set and no Elixir is on this bench")
	}
	t.Skip("TestGuardKindAndScalarVersionUnitsElixir: no Elixir (dist/ or PATH); run under SCHEMA_REQUIRE_CORPUS=1 on a bench that has it")
	return ""
}

// writeVersionModules writes the generated .ex files and returns them in load
// order: FixedRuntime.ex, then BuildVersion.ex (named at compile time by the
// cook and block modules), then the rest sorted.
func writeVersionModules(t *testing.T, dir string, files map[string][]byte) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rank := func(n string) int {
		switch n {
		case "FixedRuntime.ex":
			return 0
		case "BuildVersion.ex":
			return 1
		}
		return 2
	}
	sort.Slice(names, func(i, j int) bool {
		if rank(names[i]) != rank(names[j]) {
			return rank(names[i]) < rank(names[j])
		}
		return names[i] < names[j]
	})
	return names
}

func runVersionProbe(t *testing.T, elixirBin, dir string, names []string, probe string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, n := range names {
		args = append(args, "-r", n)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(elixirBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	return string(out)
}
