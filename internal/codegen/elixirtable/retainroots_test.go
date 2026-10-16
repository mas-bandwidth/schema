package elixirtable

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestRetainsAndWidensOnEveryRootElixir pins docs/SPEC-TABLES.md §4, §4.2 and
// §6.6 over EVERY ROOT the conformance manifest names that the Elixir leg
// carries: each `instance` and `report` wire of
// testdata/conformance/tables/MANIFEST.txt, read by the generated Elixir and by
// the reference oracle (internal/tablewire), and held to it.
//
//   - RETAIN-UNKNOWN (§6.6): a variable-class root is loaded through
//     load_retain at the wire fuzzer's capacities and written back through
//     save_retain; the retained, retain_lost and unknown counters and the
//     retained save are the oracle's, byte for byte.
//   - WIDENING ON READ (§4): every case's plain load reports the oracle's
//     widened, clamped, unknown and mismatched counts, and the value it read
//     saves to the oracle's bytes; the field_scalar_widen row is the direct
//     widening case and must widen exactly once and read cleanly.
//   - THE REASON IN THE REPORT (§4, §4.2): the load report's :reason is the
//     reference leg's TableRefuseReason name — ok for every read that finished,
//     unknown_form for a form byte this reader does not carry, wire_damaged
//     for framing damage.
//
// A unit the Elixir emitter refuses by name and a root its codec marks
// uncarried are COUNTED and NAMED in the log, never silently passed.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestRetainsAndWidensOnEveryRootElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	units, cases := readConformanceManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))

	byUnit := map[string][]manifestCase{}
	for _, c := range cases {
		byUnit[c.unit] = append(byUnit[c.unit], c)
	}
	keys := make([]string, 0, len(byUnit))
	for k := range byUnit {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var refusedUnits, uncarried []string
	checked, retainedSomewhere, widenedDirect := 0, false, false
	for _, key := range keys {
		path, ok := units[key]
		if !ok {
			t.Fatalf("manifest case names unit %s with no unit line", key)
		}
		if !elixirCorpusUnits[key] {
			// NOT IN THE LEG'S CORPUS (make/elixir.mk ELIXIR_TABLE_UNITS): the
			// emitter refuses it by name (§11) or its codec is owed: absent, named
			refusedUnits = append(refusedUnits, key)
			continue
		}
		u := loadUnitPath(t, filepath.Join(root, path))
		files, gerr := generateElixirErr(u)
		if gerr != nil {
			t.Errorf("unit %s: the corpus lists it and the emitter refused it: %v", key, gerr)
			continue
		}
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)
		ns := elixirNamespace(t, files)
		var probe strings.Builder
		probe.WriteString("mod = " + ns + ".Table\n")
		probe.WriteString(retainRootsProbe)
		for _, c := range byUnit[key] {
			fmt.Fprintf(&probe, "RetainRoots.one(mod, %s, %s, %s)\n", elixirString(c.name), elixirString(c.root), elixirString(filepath.Join(root, c.wire)))
		}
		_ = names
		out := runProbeParallel(t, elixirBin, dir, probe.String())
		model := tabletext.NewModel(u)
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.Split(line, "|")
			if len(f) == 0 || f[0] != "case" {
				continue
			}
			name, rootName := f[1], f[2]
			if f[3] == "uncarried" {
				uncarried = append(uncarried, key+"."+rootName)
				continue
			}
			c := findCase(t, byUnit[key], name)
			data, err := os.ReadFile(filepath.Join(root, c.wire))
			if err != nil {
				t.Fatal(err)
			}
			def := model.Lookup(rootName)
			if def == nil {
				t.Fatalf("%s: unit %s declares no table %s", name, key, rootName)
			}
			checkPlainAgainstOracle(t, model, def, name, data, f, f[3] == "variable")
			if name == "field_scalar_widen" {
				widenedDirect = true
				if f[4] != "0,0,1,0,false" || f[5] != "ok" {
					t.Errorf("field_scalar_widen: the direct widening case reports %s reason %s, want one widened field, a clean read and :ok", f[4], f[5])
				}
			}
			if f[3] == "variable" {
				if retained := checkRetainAgainstOracle(t, model, def, name, data, f); retained > 0 {
					retainedSomewhere = true
				}
			}
			checked++
		}
	}
	t.Logf("checked %d cases against the oracle; units the emitter refuses by name (absent): %v; roots the codec leaves uncarried (absent): %v",
		checked, refusedUnits, uncarried)
	if checked == 0 {
		t.Fatal("no manifest case reached the Elixir leg: the roster, not the property, is what broke")
	}
	if !widenedDirect {
		t.Log("the manifest's widening rows (field_scalar_widen, a1_arm_moved_width_as_a2) ride uncarried or refused roots; the direct widening case below holds the rule")
	}
	directWidening(t, elixirBin)
	if !retainedSomewhere {
		t.Log("no carried case retains a field today: retention over the manifest rides the tblrt1 rows, whose root reaches a map and is uncarried")
	}

	// THE REASON ON THE REFUSING PATHS (§3, §4.2), on a carried root
	reasonProbe(t, elixirBin, root)
}

// checkPlainAgainstOracle holds one case's plain load to the oracle: the
// counters, the value it saves, and the reason.
//
// THE SAVED VALUE IS HELD ON A VARIABLE-CLASS ROOT. A fixed-class root read on
// the variable wire is the fixed class on the table wire's own row, and its
// save is logged here, not failed: tblp3's evolution rows save 12 bytes the
// oracle does not, which is that row's to settle.
func checkPlainAgainstOracle(t *testing.T, model *tabletext.Model, def *ir.Struct, name string, data []byte, f []string, holdValue bool) {
	t.Helper()
	inst := model.New(def)
	var rep tabletext.Report
	ok, derr := tablewire.DecodeRegion(model, inst, data, &rep)
	if derr != nil {
		t.Errorf("%s: the oracle refused the committed wire: %v", name, derr)
		return
	}
	malformed := rep.Malformed || !ok
	want := fmt.Sprintf("%d,%d,%d,%d,%t", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, malformed)
	if f[4] != want {
		t.Errorf("%s: the Elixir load reports %s (unknown,kind_mismatch,widened,clamped,malformed), the oracle %s", name, f[4], want)
	}
	wantReason := "ok"
	if malformed {
		wantReason = "wire_damaged"
	}
	if f[5] != wantReason {
		t.Errorf("%s: the load report's reason is %s, want %s (gotable/refusal.go)", name, f[5], wantReason)
	}
	encoded, eerr := tablewire.Encode(model, inst)
	if eerr != nil {
		if f[6] != "refused" {
			t.Errorf("%s: the oracle refused to encode; the leg saved %d bytes", name, len(f[6])/2)
		}
		return
	}
	if f[6] != hex.EncodeToString(encoded) {
		if holdValue {
			t.Errorf("%s: the value the Elixir load read saves %d bytes, the oracle's %d", name, len(f[6])/2, len(encoded))
		} else {
			t.Logf("%s (fixed-class root, the fixed class's row): the Elixir save is %d bytes, the oracle's %d", name, len(f[6])/2, len(encoded))
		}
	}
}

// checkRetainAgainstOracle holds one variable-class case's retaining pair to
// the oracle's at the wire fuzzer's capacities (test/conformance/harness/
// wirefuzz.go), and returns the retained count.
func checkRetainAgainstOracle(t *testing.T, model *tabletext.Model, def *ir.Struct, name string, data []byte, f []string) int {
	t.Helper()
	inst := model.New(def)
	var rep tabletext.Report
	retain := tablewire.Retain{Capacity: 1 << 20, IdCapacity: (1 << 20) / 8}
	if _, err := tablewire.DecodeRegionRetain(model, inst, data, &retain, &rep); err != nil {
		t.Errorf("%s: the oracle's retaining load refused the committed wire: %v", name, err)
		return 0
	}
	var saved tabletext.Report
	out, err := tablewire.EncodeRetain(model, inst, &retain, &saved)
	if err != nil {
		t.Errorf("%s: the oracle's retaining save refused: %v", name, err)
		return 0
	}
	want := fmt.Sprintf("%d,%d,%d", rep.Retained, rep.RetainLost+saved.RetainLost, rep.Unknown)
	if f[7] != want {
		t.Errorf("%s: the Elixir retaining pair reports %s (retained,retain_lost,unknown), the oracle %s", name, f[7], want)
	}
	if f[8] != hex.EncodeToString(out) {
		t.Errorf("%s: the Elixir retained save is %d bytes, the oracle's %d", name, len(f[8])/2, len(out))
	}
	return rep.Retained
}

// directWidening is the DIRECT widening case (docs/SPEC-TABLES.md §4): a
// writer declares n int16 and a reader int32 under the same name, so the field
// id is one and the wire kind is narrower. The writer's generated Elixir saves
// n = -7; the reader's generated Elixir loads it, widens it once and reads it
// cleanly with reason ok, and the oracle reading the same bytes into the
// reader's declaration agrees on the counters and on the bytes the value saves.
func directWidening(t *testing.T, elixirBin string) {
	t.Helper()
	writer := unitFrom(t, "package widen\n\ntable Sample\n{\n    n int16\n    s string(8)\n    xs []int16\n}\n")
	reader := unitFrom(t, "package widen\n\ntable Sample\n{\n    n int32\n    s string(8)\n    xs []int32\n}\n")
	wdir := t.TempDir()
	wfiles, err := elixirGenerateUnit(writer)
	if err != nil {
		t.Fatalf("the writer: %v", err)
	}
	writeModuleDir(t, wdir, wfiles)
	wire := filepath.Join(t.TempDir(), "narrow.wire")
	runProbeParallel(t, elixirBin, wdir, `{:ok, bytes} = Widen.Table.save("Sample", %{"n" => -7, "s" => "hi", "xs" => [-1, 2]})
File.write!(`+elixirString(wire)+`, bytes)
`)
	rdir := t.TempDir()
	rfiles, err := elixirGenerateUnit(reader)
	if err != nil {
		t.Fatalf("the reader: %v", err)
	}
	writeModuleDir(t, rdir, rfiles)
	out := runProbeParallel(t, elixirBin, rdir, `wire = File.read!(`+elixirString(wire)+`)
{:ok, v, r} = Widen.Table.load("Sample", wire)
{:ok, saved} = Widen.Table.save("Sample", v)
IO.puts("widen|#{v["n"]}|#{inspect(v["xs"])}|#{r.unknown},#{r.kind_mismatch},#{r.widened},#{r.clamped},#{r.malformed}|#{r.reason}|#{Base.encode16(saved, case: :lower)}")
`)
	var line []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "widen|") {
			line = strings.Split(l, "|")
		}
	}
	if len(line) != 6 {
		t.Fatalf("the direct widening probe answered no line:\n%s", out)
	}
	if line[1] != "-7" || line[2] != "[-1, 2]" {
		t.Errorf("the widened values read %s and %s, want -7 and [-1, 2]", line[1], line[2])
	}
	if line[3] != "0,0,2,0,false" || line[4] != "ok" {
		t.Errorf("the direct widening case reports %s reason %s, want two widened fields (the scalar and the array's elements), a clean read and :ok", line[3], line[4])
	}
	data, err := os.ReadFile(wire)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(reader)
	inst := model.New(model.Lookup("Sample"))
	var rep tabletext.Report
	if _, err := tablewire.DecodeRegion(model, inst, data, &rep); err != nil {
		t.Fatalf("the oracle refused the narrow wire: %v", err)
	}
	if got := fmt.Sprintf("%d,%d,%d,%d,%t", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Malformed); got != line[3] {
		t.Errorf("the oracle reads the narrow wire as %s, the Elixir reader as %s", got, line[3])
	}
	encoded, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(encoded) != line[5] {
		t.Errorf("the widened value saves %d bytes in Elixir, %d in the oracle", len(line[5])/2, len(encoded))
	}
}

// reasonProbe drives the refusing paths on graphdemo's Scene: a clean read
// says ok, a form byte this reader does not carry says unknown_form, and an
// empty wire and a truncated one say wire_damaged with malformed set.
func reasonProbe(t *testing.T, elixirBin, root string) {
	t.Helper()
	u := loadUnitPath(t, filepath.Join(root, "tables", "pointers"))
	files := generateElixir(t, u)
	dir := t.TempDir()
	writeModuleDir(t, dir, files)
	wire := filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin")
	probe := `
wire = File.read!(` + elixirString(wire) + `)
t = Graphdemo.Table
{:ok, _v, r} = t.load("Scene", wire)
IO.puts("clean|#{r.reason}|#{r.malformed}")
{:error, :newer_form, r} = t.load("Scene", <<2>> <> binary_part(wire, 1, byte_size(wire) - 1))
IO.puts("form2|#{r.reason}|#{r.malformed}")
{:error, :malformed, r} = t.load("Scene", <<>>)
IO.puts("empty|#{r.reason}|#{r.malformed}")
case t.load("Scene", binary_part(wire, 0, 5)) do
  {:error, _, r} -> IO.puts("short|#{r.reason}|#{r.malformed}")
  {:ok, _, r} -> IO.puts("short|#{r.reason}|#{r.malformed}")
end
`
	out := runProbeParallel(t, elixirBin, dir, probe)
	for _, want := range []string{"clean|ok|false", "form2|unknown_form|false", "empty|wire_damaged|true", "short|wire_damaged|true"} {
		if !strings.Contains(out, want) {
			t.Errorf("the load report's reason: want the line %q in\n%s", want, out)
		}
	}
}

// retainRootsProbe answers one manifest case per line:
// case|<name>|<root>|<class>|<u,k,w,c,malformed>|<reason>|<saved hex>|<retained,retain_lost,unknown>|<retained save hex>
const retainRootsProbe = `
defmodule RetainRoots do
  def one(mod, name, root, path) do
    wire = File.read!(path)

    cond do
      not mod.carried?(root) ->
        IO.puts("case|#{name}|#{root}|uncarried")

      true ->
        class = if mod.variable?(root), do: "variable", else: "fixed"
        {_verdict, value, r} = mod.load_tolerant(root, wire)
        counters = "#{r.unknown},#{r.kind_mismatch},#{r.widened},#{r.clamped},#{r.malformed}"

        saved =
          case mod.save(root, value) do
            {:ok, bytes} -> Base.encode16(bytes, case: :lower)
            _ -> "refused"
          end

        {retained, retained_save} =
          if class == "variable" do
            retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
            report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}

            case mod.load_retain(root, wire, retain, report) do
              {:ok, v, rl, retain} ->
                {:ok, out, rs, _} = mod.save_retain(root, v, retain, rl)
                {"#{rs.retained},#{rs.retain_lost},#{rl.unknown}", Base.encode16(out, case: :lower)}

              _ ->
                {"refused", "refused"}
            end
          else
            {"-", "-"}
          end

        IO.puts("case|#{name}|#{root}|#{class}|#{counters}|#{r.reason}|#{saved}|#{retained}|#{retained_save}")
    end
  end
end
`

// elixirCorpusUnits is the manifest's units the Elixir corpus compiles
// (make/elixir.mk, ELIXIR_TABLE_UNITS): the units the emitter refuses by name
// (a union in a table closure, a payload-free arm, `was` on a variant or arm)
// and tblw1, tblw2 and tblg1, whose codecs are owed, are absent and named.
var elixirCorpusUnits = map[string]bool{
	"tabledemo": true, "graphdemo": true, "blockdemo": true, "tblv1": true, "tblv2": true,
	"tblp1": true, "tblp2": true, "tblp3": true, "blobdemo": true, "listdemo": true,
	"tblk1": true, "tblk2": true, "scalars": true, "tblscalars2": true,
	"vocabdemo": true, "vocab9demo": true, "tblrt1": true, "tblrt2": true, "tblrt3": true,
	"mapdemo": true,
}

type manifestCase struct{ name, unit, root, wire string }

// readConformanceManifest reads the unit lines and the instance and report
// cases of the conformance manifest (testdata/conformance/tables/FORMAT.md).
func readConformanceManifest(t *testing.T, path string) (map[string]string, []manifestCase) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string]string{}
	var cases []manifestCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			units[fields[1]] = fields[2]
		case "instance", "report":
			if len(fields) < 5 {
				t.Fatalf("%s: short %s line %q", path, fields[0], sc.Text())
			}
			cases = append(cases, manifestCase{name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return units, cases
}

func findCase(t *testing.T, cases []manifestCase, name string) manifestCase {
	t.Helper()
	for _, c := range cases {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("the probe answered %s, which the manifest does not name", name)
	return manifestCase{}
}

// loadUnitPath loads a manifest unit: a schema file, or every schema file in a
// directory.
func loadUnitPath(t *testing.T, path string) *ir.Unit {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		return loadUnit(t, path)
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.schema"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("%s: no schema files (%v)", path, err)
	}
	sort.Strings(matches)
	return loadUnit(t, matches...)
}

// generateElixirErr is generateElixir that returns the emitter's refusal
// rather than failing the test on it.
func generateElixirErr(u *ir.Unit) (files map[string][]byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the emitter panicked: %v", p)
		}
	}()
	files, err = elixirGenerateUnit(u)
	return files, err
}

// elixirNamespace reads the namespace the generated Table module declares.
func elixirNamespace(t *testing.T, files map[string][]byte) string {
	t.Helper()
	src, ok := files[VariableTableModule+".ex"]
	if !ok {
		t.Fatal("the unit emitted no Table.ex")
	}
	for _, line := range strings.Split(string(src), "\n") {
		if rest, ok := strings.CutPrefix(line, "defmodule "); ok {
			name := strings.TrimSuffix(strings.Fields(rest)[0], "."+VariableTableModule)
			if _, err := strconv.Atoi(name); err == nil {
				t.Fatalf("namespace %q", name)
			}
			return name
		}
	}
	t.Fatal("Table.ex declares no module")
	return ""
}

// runProbeParallel compiles the unit's modules with elixirc, which orders a
// multi-file unit's compile-time dependencies itself (tabledemo's nested fixed
// structs name each other), exactly as make/elixir.mk builds the corpus, and
// runs the probe over that ebin.
func runProbeParallel(t *testing.T, elixirBin, dir, probe string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	sources, err := filepath.Glob(filepath.Join(dir, "*.ex"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("%s: no generated modules (%v)", dir, err)
	}
	ebin := filepath.Join(dir, "ebin")
	if err := os.MkdirAll(ebin, 0o755); err != nil {
		t.Fatal(err)
	}
	compile := exec.Command(filepath.Join(filepath.Dir(elixirBin), "elixirc"), append([]string{"-o", ebin}, sources...)...)
	compile.Dir = dir
	compile.Env = env
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("elixirc failed: %v\n%s", err, out)
	}
	cmd := exec.Command(elixirBin, "-pa", ebin, "probe.exs")
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// elixirGenerateUnit is generateElixir's two emitters, returning a refusal.
func elixirGenerateUnit(u *ir.Unit) (map[string][]byte, error) {
	files, err := elixir.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range tables {
		files[k] = v
	}
	return files, nil
}
