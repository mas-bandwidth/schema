package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// unionElemResetMutant is the wire fuzzer's duplicate-damaged mutant of
// message_batch (messagedemo.ToolMessage), run seed 1, mutant 24538 of the
// tables-squirrel-release campaign. Transaction.pending, a FIXED [2] array of
// EditBody unions, rides twice; the second copy's first element carries an arm
// length its body cannot cover.
const unionElemResetMutant = "0101080b000000020f030d9101040c056261746368050e410f02060d3b070d0d080807000000090801000000000a0c02796f0b0e220f020c0d090d0c05726f77616e000e0d110f0c0767656e2e6c7561080803000000000000050e410f02060d7f070d0d080807000000090801000000000a0c02796f0b0e220f020c0d090d0c05726f77616e000e0d110f0c0767656e2e6c756108080300000000000000100e0d0f0200110d0712082a0000000000a828f581a4ac38aa9532c9c69be74dcd87f56fdcf4bb96ed39264f47f3d7e8653c08b5eed6a0de522862915987c6717268a945b5074e9c0807594f69ada54bbf87f28b628712535f7e409519eff404fa743b118166d83744f2482b03e480677d861b638ebaadbcc44a63043c2982fcac7606d7eb0d2dc503fff2d5bb45894233a90733c50de030bfd8c00dd6714ca9731200000000000000"

// armWidenDamagedMutant is the kind pass's mutant #228 of message_arms
// (messagedemo.ToolMessage), run seed 1, mutant 27132 of the same campaign:
// a history element's arm kind 9 is swapped for kind 6, which widens to it,
// but the arm's L still spells the 8 bytes of kind 9.
const armWidenDamagedMutant = "0101080c000000020f030e0a0402fdffffff09000000040e170f020506080300000000000000060f070708042900000000a828f581a4ac38aa9532c9c69be74dcd166856b28afc7d43fff2d5bb45894233f85775e8906b5ab5cf145a78fee4eab9220160561970af770700000000000000"

// TestSquirrelUnionElementResetsAtItsArmID pins the first mutant. An element of an
// array of unions is RESET the moment its arm id is read, before the kind and
// the arm length are checked (docs/SPEC-TABLES.md §4), so the repeat leaves no
// arm the first occurrence decoded standing and pending[0] reads None. The
// load then save must equal the Go engine's encoding of the same bytes; a leg
// that kept the first occurrence's arm resaves the unmutated seed instead.
func TestSquirrelUnionElementResetsAtItsArmID(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	unit := loadUnit(t, root, []string{"tables/messages"})
	data, err := hex.DecodeString(unionElemResetMutant)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(unit)
	ans, err := fuzzEngine(m, m.Lookup("ToolMessage"), m.IsVariable("ToolMessage"), data)
	if err != nil {
		t.Fatal(err)
	}
	if ans.saveFail || !ans.malformed {
		t.Fatalf("the engine's answer moved: saveFail=%t malformed=%t", ans.saveFail, ans.malformed)
	}

	dir := t.TempDir()
	mod := emitUnit(t, dir, "messagedemo", unit)
	var p strings.Builder
	p.WriteString(savePrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "messagedemo", mod, "ToolMessage", "case")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data}))["case"]
	if want := hex.EncodeToString(ans.saved); got.hex != want {
		t.Fatalf("the load then save wrote %d bytes, the engine %d\n got %s\nwant %s", len(got.hex)/2, len(ans.saved), got.hex, want)
	}
}

// TestSquirrelArmWidenedCountsOnlyADecodedPayload pins the second mutant. A
// widened arm counts `widened` only once its payload decodes at its own width
// (docs/SPEC-TABLES.md §4): an L that is not that width is the arm's framing
// damage, `malformed` alone, and the element reads None.
func TestSquirrelArmWidenedCountsOnlyADecodedPayload(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	unit := loadUnit(t, root, []string{"tables/messages"})
	data, err := hex.DecodeString(armWidenDamagedMutant)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(unit)
	ans, err := fuzzEngine(m, m.Lookup("ToolMessage"), m.IsVariable("ToolMessage"), data)
	if err != nil {
		t.Fatal(err)
	}
	if ans.widened != 0 || !ans.malformed {
		t.Fatalf("the engine's answer moved: widened=%d malformed=%t", ans.widened, ans.malformed)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "messagedemo", unit)
	var p strings.Builder
	p.WriteString(loadPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "messagedemo", mod, "ToolMessage", "case")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := reportLine(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data}))
	if want := "0,0,0,0,0,true"; !strings.HasPrefix(got, want) {
		t.Fatalf("the load reports %q, the engine %s (unknown, kind_mismatch, widened, clamped, duplicate, malformed)", got, want)
	}
}

// widenInto128Mutant is the widen pass's mutant #0 of scalars_full
// (scalars.SimState), run seed 1, mutant 32990 of the same campaign: a
// narrower integer kind rides where the reader declares a 128-bit one.
const widenInto128Mutant = "01011481021601004cff031700802f7500000000041800c0c0bdf0ffffffffffffffffffffff051640420f000619ef071b0000e703081cfffffeffffffffff091d00807f841e00000000000000000000000a1bffffffff0b02000c12fff1052a0100000000000000000000000d13ffffffffffffffffffffffffffffffff0e160080ffff0f0e0e1603008001000000f8ffffff0700100e061a028000006411101617021208000000009cffffff1308ffffffff63000000140e2213020100000000000000000000000000000000000000000000000100000000000000150d1b16170080ffffffffffff171700400c0000000000181bffff670100190d07181b000001000000c6af9b2eef88501e38861506cdb81a404ad7a1fc263abf4c58b8de05f3a11d89b5253e6a6c26d97fe4ad77811124c9fb400e20a08a498122e1d03c0919c07d8bb10a7bcea257371f6fe6b4286790e5234226af0879dd1bd6152fef40914ffdf21267e37866fdfc23911d1ab7fbb9ac6adcdd483b6acab1e31e418af06e4b49b1b8679c0d849e9632674d02864c1564afcd5002864c1764af6de14a1b30acf15a647095a80dd82f8c071702864cf563af541502864cf463af750fca90918228b1981f0eb28af728431900000000000000"

// TestSquirrelIntegerWidensInto128Bits pins the third mutant. A narrower
// integer widened into an int128 or uint128 field lands as that field's own
// 128-bit value, sign- or zero-extended (docs/SPEC-TABLES.md §4); a leg that
// stored the narrow scalar crashed on save. The load then save must equal the
// Go engine's encoding of the same bytes.
func TestSquirrelIntegerWidensInto128Bits(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	unit := loadUnit(t, root, []string{"tables/scalars"})
	data, err := hex.DecodeString(widenInto128Mutant)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(unit)
	ans, err := fuzzEngine(m, m.Lookup("SimState"), m.IsVariable("SimState"), data)
	if err != nil {
		t.Fatal(err)
	}
	if ans.saveFail || ans.widened == 0 {
		t.Fatalf("the engine's answer moved: saveFail=%t widened=%d", ans.saveFail, ans.widened)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "scalars", unit)
	var p strings.Builder
	p.WriteString(savePrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "scalars", mod, "SimState", "case")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data}))["case"]
	if want := hex.EncodeToString(ans.saved); got.hex != want {
		t.Fatalf("the load then save wrote %d bytes, the engine %d\n got %s\nwant %s", len(got.hex)/2, len(ans.saved), got.hex, want)
	}
}
