package elixirtable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadUnitFromFile(t *testing.T, path string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatalf("gather paths %s: %v", path, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return u
}

func loadUnitFromSource(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return loadUnitFromFile(t, path)
}

// TestDefaultsAndTableRenameElixir pins docs/SPEC-TABLES.md §3, §4.2, §5 and
// §16 for vt/defaults-and-table-rename/elixir:
//   - a declared string, bytes and flags default each writes and reads as the
//     Go leg does on the variable wire and in the text form (including inside a
//     by-value nested table/type and a pointed-to table node, and when
//     restoring defaults after wire damage or a text kind mismatch);
//   - an older writer's bytes (tblw1 Vessel) read across a table rename (tblw2
//     Ship | was = "Vessel") equal the engine's read in both directions, and
//     stripping `was` leaves the renamed node records unknown.
//
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestDefaultsAndTableRenameElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	w1Unit := loadUnitFromFile(t, filepath.Join(root, "test", "tables", "W1.schema"))
	w2Unit := loadUnitFromFile(t, filepath.Join(root, "test", "tables", "W2.schema"))

	w2Raw, err := os.ReadFile(filepath.Join(root, "test", "tables", "W2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	w2NoWasSrc := strings.Replace(
		 strings.Replace(string(w2Raw), "package tblw2", "package tblw2nowas", 1),
		`fixed table Ship | was = "Vessel"`,
		`fixed table Ship`,
		1,
	)
	w2NoWasUnit := loadUnitFromSource(t, "W2NoWas.schema", w2NoWasSrc)

	dir := t.TempDir()
	var builds, rest []string
	for _, entry := range []struct {
		name string
		unit *ir.Unit
	}{
		{"tblw1", w1Unit},
		{"tblw2", w2Unit},
		{"tblw2nowas", w2NoWasUnit},
	} {
		out, err := compiler.New().Generate(entry.unit, "elixir", compiler.Options{})
		if err != nil {
			t.Fatalf("schema generate --lang elixir %s refused: %v", entry.name, err)
		}
		sub := filepath.Join(dir, entry.name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range elixirtable.WriteModuleDirForTest(t, sub, out) {
			rel := filepath.Join(entry.name, n)
			if n == "BuildVersion.ex" {
				builds = append(builds, rel)
			} else {
				rest = append(rest, rel)
			}
		}
	}
	names := append(builds, rest...)

	// Oracle 1: default Vessel and default Fleet from the Go engine.
	m1 := tabletext.NewModel(w1Unit)
	vesselDefInst := m1.New(m1.Lookup("Vessel"))
	vesselDefWire, err := tablewire.Encode(m1, vesselDefInst)
	if err != nil {
		t.Fatalf("encode default Vessel: %v", err)
	}
	vesselDefJSON, err := m1.Write(vesselDefInst)
	if err != nil {
		t.Fatalf("write default Vessel json: %v", err)
	}

	fleetDefInst := m1.New(m1.Lookup("Fleet"))
	fleetDefWire, err := tablewire.Encode(m1, fleetDefInst)
	if err != nil {
		t.Fatalf("encode default Fleet: %v", err)
	}
	fleetDefJSON, err := m1.Write(fleetDefInst)
	if err != nil {
		t.Fatalf("write default Fleet json: %v", err)
	}

	// Oracle 2: cleared/zero Vessel (every field off its non-zero declared default).
	vesselZeroInput := []byte(`{"name":"","tag":"","caps":[],"badge":{"label":""},"hull":0}`)
	vesselZeroInst := m1.New(m1.Lookup("Vessel"))
	var rep tabletext.Report
	if !m1.Read(vesselZeroInst, vesselZeroInput, &rep) || !rep.Silent() {
		t.Fatalf("engine read zero Vessel: %+v", rep)
	}
	vesselZeroWire, err := tablewire.Encode(m1, vesselZeroInst)
	if err != nil {
		t.Fatalf("encode zero Vessel: %v", err)
	}
	vesselZeroJSON, err := m1.Write(vesselZeroInst)
	if err != nil {
		t.Fatalf("write zero Vessel json: %v", err)
	}

	// Oracle 3: damaged/mismatched wires for default recovery on Vessel.
	// 3a: name written as "ok", then repeated with invalid UTF-8 0xff -> malformed, name restores "untitled".
	vesselFields := w1Unit.Tables["Vessel"].Fields
	nameID := ir.TableFieldWireId(vesselFields[0])
	tagID := ir.TableFieldWireId(vesselFields[1])
	capsID := ir.TableFieldWireId(vesselFields[2])

	damagedNameWire := []byte{1, 1, 12, 2, 'o', 'k', 1, 12, 1, 0xff, 0}
	damagedNameWire = binary.LittleEndian.AppendUint64(damagedNameWire, nameID)
	damagedNameWire = binary.LittleEndian.AppendUint64(damagedNameWire, 1)

	// 3b: tag (bytes(4) = "ab") arrives with wrong element kind 4 (int32) instead of 6 (uint8) -> kind_mismatch, tag stays "ab".
	badTagWire := []byte{1, 1, 14, 2, 4, 0, 0}
	badTagWire = binary.LittleEndian.AppendUint64(badTagWire, tagID)
	badTagWire = binary.LittleEndian.AppendUint64(badTagWire, 1)

	// 3c: caps (Caps = { Jump, Fly }) arrives with wrong kind 12 (string) instead of 6 (uint8) -> kind_mismatch, caps stays 5.
	badCapsWire := []byte{1, 1, 12, 0, 0}
	badCapsWire = binary.LittleEndian.AppendUint64(badCapsWire, capsID)
	badCapsWire = binary.LittleEndian.AppendUint64(badCapsWire, 1)

	// Oracle 4: older writer's bytes (w1_fleet.bin) decoded by the engine under W2 (Ship | was = "Vessel").
	w1FleetPath := filepath.Join(root, "testdata", "wire", "tables", "w1_fleet.bin")
	w1FleetWire, err := os.ReadFile(w1FleetPath)
	if err != nil {
		t.Fatal(err)
	}
	w2FleetPath := filepath.Join(root, "testdata", "wire", "tables", "w2_fleet.bin")
	m2 := tabletext.NewModel(w2Unit)
	w1AsW2Inst := m2.New(m2.Lookup("Fleet"))
	var rW2 tabletext.Report
	if ok, err := tablewire.Decode(m2, w1AsW2Inst, w1FleetWire, &rW2); err != nil || !ok || !rW2.Silent() {
		t.Fatalf("engine decode w1_fleet as w2: ok=%v err=%v report=%+v", ok, err, rW2)
	}
	w1AsW2EngineWire, err := tablewire.Encode(m2, w1AsW2Inst)
	if err != nil {
		t.Fatalf("engine encode w1_fleet as w2: %v", err)
	}
	w1AsW2EngineJSON, err := m2.Write(w1AsW2Inst)
	if err != nil {
		t.Fatalf("engine write w1_fleet as w2 json: %v", err)
	}

	writeFixture := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	pVesselDefWire := writeFixture("vessel_def.bin", vesselDefWire)
	pVesselDefJSON := writeFixture("vessel_def.json", append(vesselDefJSON, 10))
	pFleetDefWire := writeFixture("fleet_def.bin", fleetDefWire)
	pFleetDefJSON := writeFixture("fleet_def.json", append(fleetDefJSON, 10))
	pVesselZeroWire := writeFixture("vessel_zero.bin", vesselZeroWire)
	pVesselZeroJSON := writeFixture("vessel_zero.json", append(vesselZeroJSON, 10))
	pDamagedNameWire := writeFixture("damaged_name.bin", damagedNameWire)
	pBadTagWire := writeFixture("bad_tag.bin", badTagWire)
	pBadCapsWire := writeFixture("bad_caps.bin", badCapsWire)
	pW1AsW2EngineWire := writeFixture("w1_as_w2_engine.bin", w1AsW2EngineWire)
	pW1AsW2EngineJSON := writeFixture("w1_as_w2_engine.json", append(w1AsW2EngineJSON, 10))

	probe := fmt.Sprintf(`
defmodule DefaultsRenameProbe do
  def check(true, _what), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def silent?(r) do
    r.malformed == false and r.unknown == 0 and r.kind_mismatch == 0 and r.clamped == 0 and r.widened == 0
  end

  def run do
    want_vessel_def = %%{
      "name" => "untitled",
      "tag" => "ab",
      "caps" => 5,
      "badge" => %%{"label" => "new"},
      "hull" => 100
    }

    # 1. Declared string, bytes and flags defaults on the variable wire and text form (Vessel).
    vessel_def_wire = File.read!(%[1]q)
    vessel_def_json = File.read!(%[2]q)
    {:ok, v_wire, r1} = Tblw1.Table.load("Vessel", vessel_def_wire)
    check(silent?(r1), "Vessel default wire report: " <> inspect(r1))
    check(v_wire == want_vessel_def, "Vessel default wire value: got " <> inspect(v_wire) <> " want " <> inspect(want_vessel_def))

    {:ok, v_json, r2} = Tblw1.Table.from_json("Vessel", "{}")
    check(silent?(r2), "Vessel empty json report: " <> inspect(r2))
    check(v_json == want_vessel_def, "Vessel empty json value: got " <> inspect(v_json) <> " want " <> inspect(want_vessel_def))

    {:ok, v_saved} = Tblw1.Table.save("Vessel", v_wire)
    check(v_saved == vessel_def_wire, "Vessel default save elides to 10-byte wire: got " <> inspect(v_saved))
    {:ok, v_text} = Tblw1.Table.to_json("Vessel", v_wire)
    check(v_text == vessel_def_json, "Vessel default to_json matches Go engine:\n" <> v_text)

    # 2. Declared defaults inside a by-value nested table (Fleet.home) on wire and text.
    fleet_def_wire = File.read!(%[3]q)
    fleet_def_json = File.read!(%[4]q)
    want_fleet_def = %%{
      "title" => "fleet",
      "flagship" => nil,
      "escorts" => [],
      "home" => want_vessel_def
    }
    {:ok, f_wire, r3} = Tblw1.Table.load("Fleet", fleet_def_wire)
    check(silent?(r3), "Fleet default wire report: " <> inspect(r3))
    check(f_wire == want_fleet_def, "Fleet default wire value (nested defaults): got " <> inspect(f_wire) <> " want " <> inspect(want_fleet_def))

    {:ok, f_json, r4} = Tblw1.Table.from_json("Fleet", "{}")
    check(silent?(r4), "Fleet empty json report: " <> inspect(r4))
    check(f_json == want_fleet_def, "Fleet empty json value (nested defaults): got " <> inspect(f_json) <> " want " <> inspect(want_fleet_def))

    {:ok, f_saved} = Tblw1.Table.save("Fleet", f_wire)
    check(f_saved == fleet_def_wire, "Fleet default save elides to 10-byte wire: got " <> inspect(f_saved))
    {:ok, f_text} = Tblw1.Table.to_json("Fleet", f_wire)
    check(f_text == fleet_def_json, "Fleet default to_json matches Go engine:\n" <> f_text)

    # 3. Cleared/zero values (empty string, empty bytes, zero flags) write onto the wire and read back as empty/zero, not defaults.
    vessel_zero_wire = File.read!(%[5]q)
    vessel_zero_json = File.read!(%[6]q)
    want_vessel_zero = %%{
      "name" => "",
      "tag" => <<>>,
      "caps" => 0,
      "badge" => %%{"label" => ""},
      "hull" => 0
    }
    {:ok, z_saved} = Tblw1.Table.save("Vessel", want_vessel_zero)
    check(z_saved == vessel_zero_wire, "Vessel zero save matches Go engine wire")
    {:ok, z_loaded, rz1} = Tblw1.Table.load("Vessel", z_saved)
    check(silent?(rz1) and z_loaded == want_vessel_zero, "Vessel zero load preserves empty/zero over nonzero defaults: " <> inspect(z_loaded))
    {:ok, z_text} = Tblw1.Table.to_json("Vessel", z_loaded)
    check(z_text == vessel_zero_json, "Vessel zero to_json matches Go engine")
    {:ok, z_from_json, rz2} = Tblw1.Table.from_json("Vessel", z_text)
    check(silent?(rz2) and z_from_json == want_vessel_zero, "Vessel zero from_json preserves empty/zero: " <> inspect(z_from_json))

    # 4. Default recovery on damaged wire and mismatched JSON fields.
    {:ok, d_name, rd1} = Tblw1.Table.load("Vessel", File.read!(%[7]q))
    check(rd1.malformed == true and d_name == want_vessel_def, "damaged UTF-8 string restores declared default: got " <> inspect(d_name) <> " report " <> inspect(rd1))

    {:ok, d_tag, rd2} = Tblw1.Table.load("Vessel", File.read!(%[8]q))
    check(rd2.kind_mismatch == 1 and d_tag == want_vessel_def, "mismatched bytes element kind keeps declared default: got " <> inspect(d_tag) <> " report " <> inspect(rd2))

    {:ok, d_caps, rd3} = Tblw1.Table.load("Vessel", File.read!(%[9]q))
    check(rd3.kind_mismatch == 1 and d_caps == want_vessel_def, "mismatched flags kind keeps declared default: got " <> inspect(d_caps) <> " report " <> inspect(rd3))

    {:ok, d_json, rd4} = Tblw1.Table.from_json("Vessel", ~s({"name":123,"tag":"!not-base64!","caps":"bad","badge":{"label":456}}))
    check(rd4.kind_mismatch == 4 and d_json == want_vessel_def, "mismatched JSON fields keep declared defaults: got " <> inspect(d_json) <> " report " <> inspect(rd4))

    # 5. Table rename (Vessel -> Ship | was = "Vessel"): older writer's bytes read across the rename equal the engine's read.
    w1_fleet_wire = File.read!(%[10]q)
    w2_fleet_wire = File.read!(%[11]q)
    w1_as_w2_engine_wire = File.read!(%[12]q)
    w1_as_w2_engine_json = File.read!(%[13]q)

    {:ok, w2_from_w1, rw1} = Tblw2.Table.load("Fleet", w1_fleet_wire)
    check(silent?(rw1), "w1_fleet read by tblw2 report: " <> inspect(rw1))
    {:ok, w2_from_engine_json, rw2} = Tblw2.Table.from_json("Fleet", w1_as_w2_engine_json)
    check(silent?(rw2), "engine w2 json read by tblw2 report: " <> inspect(rw2))
    check(w2_from_w1 == w2_from_engine_json, "w1_fleet read across table rename equals engine's read: got " <> inspect(w2_from_w1) <> " want " <> inspect(w2_from_engine_json))

    {:ok, w2_resaved} = Tblw2.Table.save("Fleet", w2_from_w1)
    check(w2_resaved == w1_as_w2_engine_wire and w2_resaved == w2_fleet_wire, "w1_fleet re-saved by tblw2 matches engine and w2_fleet.bin")
    {:ok, w2_retext} = Tblw2.Table.to_json("Fleet", w2_from_w1)
    check(w2_retext == w1_as_w2_engine_json, "w1_fleet to_json by tblw2 matches engine")

    # Reverse direction: w2_fleet read by tblw1.
    {:ok, w1_from_w2, rw3} = Tblw1.Table.load("Fleet", w2_fleet_wire)
    check(silent?(rw3) and w1_from_w2 == w2_from_w1, "w2_fleet read by tblw1 equals w1_fleet read by tblw2")

    # Negative control: stripping "was = Vessel" from Ship leaves the two Vessel node records unknown.
    {:ok, w2_nowas, r_nowas} = Tblw2nowas.Table.load("Fleet", w1_fleet_wire)
    check(r_nowas.unknown == 2 and w2_nowas["flagship"] == nil and w2_nowas["escorts"] == [nil, nil],
      "stripped was on Ship must count 2 unknown node records: got " <> inspect(r_nowas) <> " value " <> inspect(w2_nowas))

    if Process.get(:failed), do: System.halt(1), else: IO.puts("DEFAULTS-AND-RENAME-OK")
  end
end

DefaultsRenameProbe.run()
`,
		pVesselDefWire,
		pVesselDefJSON,
		pFleetDefWire,
		pFleetDefJSON,
		pVesselZeroWire,
		pVesselZeroJSON,
		pDamagedNameWire,
		pBadTagWire,
		pBadCapsWire,
		w1FleetPath,
		w2FleetPath,
		pW1AsW2EngineWire,
		pW1AsW2EngineJSON,
	)

	out := elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe)
	if !strings.Contains(out, "DEFAULTS-AND-RENAME-OK") {
		t.Fatalf("probe did not complete cleanly:\n%s", out)
	}
}
