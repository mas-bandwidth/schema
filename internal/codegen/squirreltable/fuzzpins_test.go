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

// TestSquirrelUnionElementResetsAtItsArmID preserves the duplicate-damaged
// vector. Under §4 the entire read is discarded; no decoded arm is observable.
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
	if !ans.malformed {
		t.Fatalf("the engine's answer moved: saveFail=%t malformed=%t", ans.saveFail, ans.malformed)
	}

	dir := t.TempDir()
	mod := emitUnit(t, dir, "messagedemo", unit)
	requireSquirrelDiscard(t, dir, "messagedemo", mod, "ToolMessage", data)

}

// TestSquirrelArmWidenedCountsOnlyADecodedPayload preserves the wrong-width
// vector and requires whole-load discard. Counters after damage are undefined.
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
	if !ans.malformed {
		t.Fatalf("the engine's answer moved: widened=%d malformed=%t", ans.widened, ans.malformed)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "messagedemo", unit)
	requireSquirrelDiscard(t, dir, "messagedemo", mod, "ToolMessage", data)

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

// headerAndUnionPins are the release wire-fuzz gate's mutants (make
// tables-wire-fuzz-release-squirrel, plain arm) and the map and keyed header
// mutants of #2400's class. Each is pinned against the Go engine's answer on the
// same bytes, both the six counters and what the load then save writes.
var headerAndUnionPins = []struct {
	name, unit, pkg, root, wire string
}{
	{
		// seed 1, mutant 69621: Msg.body rides twice and the second copy's arm
		// reference names no entry. The repeat is framing damage that stops the
		// body before the union is touched, so the first arm stands; only an
		// ELEMENT of an array of unions resets at its arm id (§4).
		name: "a union field's damaged repeat discards the load", unit: "test/tables/M1.schema", pkg: "tblm1", root: "Msg",
		wire: "01010802000000020f030d0b040c04622e6d6405010100020fd6a461003c13e25c198a3b829532c9c6030d0b040c04622e6d6405010100003c13e25c198a3b829532c9c69be74dcd287c85bf185e6a097606d7eb0d2dc503e4ebeeb3afd85ae70500000000000000",
	},
	{
		// seed 2, mutant 60451: the same class on Cfg.effect, behind an unknown
		// field
		name: "a union field's repeat with an arm reference past the table", unit: "test/tables/R2.schema", pkg: "tblr2", root: "Cfg",
		wire: "01011e02030f040d07050a0000204000030f440d07050a0000204000060d07078a8000c03f8aea2b00080e041e0202090a1008040102040700000000d48ac477299ba8328025e5eade1ae5c39443f2513bc8c136331dea2bf696395eb1b60b3803cfc626cc69e4e29bbeb5ffc6875c803a62dc9ac599f782764e0ad9b318320d7ec916c4dd552dd0294d3fea0a00000000000000",
	},
	{
		// seed 3, mutant 73531: Cfg.podium, a fixed [3]Grade, carries L = 5 and
		// a count LEB128 that runs past it. The header is read against the
		// PARENT, so the count reads, clamps at the bound and counts `clamped`,
		// and the elements walk an empty body (§3, §4)
		name: "a positional array's count crossing its L still clamps", unit: "test/tables/V1.schema", pkg: "tblv1", root: "Cfg",
		wire: "01010409000000020a00000841031e04050c0461676564060d07070a0000a03f00080e0e04030100000014000000ff000000091e0a0b0e0480020a0c0d0e051edcf5e9f34a19030c000a0e0e1204040b00be0016000000210000002c0000000f0e0e0403050000000000000007000000100f110d07120a0000403f00008cec01864cdc63afa5f101864cdf63afb2ad1dc4a2eb3d0d8be145c2cb04da4f861b638ebaadbcc48792885f8f064c2da6ecee9a99eb45456f2c414fbf84783ed48ac477299ba832b318320d7ec916c4c599f782764e0ad991c5c4aa397792c44925d7235af46fb44656eeb16b2e8ce6dd542dd0294d3faa9443f2513bc8c136331dea2bf696395eb1b60b3803cfc6261200000000000000",
	},
	{
		// Slots.seats, a map, L = 2 under element kind 14 and a count whose
		// 0x82 continues past L: the header reads against the parent and the
		// foreign element kind is one kind_mismatch (§2.8)
		name: "a map's header is read against the parent", unit: "tables/maps", pkg: "mapdemo", root: "Slots",
		wire: "01010e020e820604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000",
	},
	{
		// KeyedConfig.teams, a keyed [Team]TeamConfig, L = 2 under element
		// kind 14 and a count past L: the keyed header reads against the parent
		// and the foreign element kind is one kind_mismatch (§3.2)
		name: "a keyed array's header is read against the parent", unit: "tables/examples", pkg: "tabledemo", root: "KeyedConfig",
		wire: "010110020e820610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a000020420d0d0100000e1a0c0a0000a0400f0a000010410d0d0a100a0000403f1101010000001207090a0000404100130d1214100e040202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0ca0c7b1daa0bc1c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bcb002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000",
	},
}

// TestSquirrelHeaderAndUnionFieldPins runs each pin through the emitted load:
// valid reads must match the engine's report and saved value. Damaged reads
// must return no value, regardless of counters accumulated before damage.
func TestSquirrelHeaderAndUnionFieldPins(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range headerAndUnionPins {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			unit := loadUnit(t, root, []string{pin.unit})
			data, err := hex.DecodeString(pin.wire)
			if err != nil {
				t.Fatal(err)
			}
			m := tabletext.NewModel(unit)
			ans, err := fuzzEngine(m, m.Lookup(pin.root), m.IsVariable(pin.root), data)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			mod := emitUnit(t, dir, pin.pkg, unit)
			if ans.malformed {
				requireSquirrelDiscard(t, dir, pin.pkg, mod, pin.root, data)
				return
			}
			var p strings.Builder
			p.WriteString(loadPrelude)
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", pin.pkg, mod, pin.root, "case")
			writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
			got := reportLine(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data}))
			want := fmt.Sprintf("%d,%d,%d,%d,%d,%t,", ans.unknown, ans.kindMismatch, ans.widened, ans.clamped, ans.duplicate, ans.malformed)
			if !strings.HasPrefix(got, want) {
				t.Fatalf("the load reports %q, the engine %s (unknown, kind_mismatch, widened, clamped, duplicate, malformed)", got, want)
			}
			p.Reset()
			p.WriteString(savePrelude)
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", pin.pkg, mod, pin.root, "case")
			writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
			saved := parseSaved(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data}))["case"]
			if w := hex.EncodeToString(ans.saved); saved.hex != w {
				t.Fatalf("the load then save wrote %d bytes, the engine %d\n got %s\nwant %s", len(saved.hex)/2, len(ans.saved), saved.hex, w)
			}
		})
	}
}

// Damaged vectors prove the verdict and absence of a usable partial result.
func requireSquirrelDiscard(t *testing.T, dir, pkg, mod, root string, data []byte) {
	t.Helper()
	program := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local S=dofile(%q,true); local T=S.require(%q); local L=S.require("schema_table_load");
local report={unknown=0,kindMismatch=0,widened=0,clamped=0,duplicate=0,malformed=false,refused=false}; local value=L.loadTolerant(T,%q,I["case"],report);
print(report.malformed && value==null ? "discarded" : "kept");`, pkg+"/schema_module.nut", mod, root)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(program))
	got := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data})))
	if got != "discarded" {
		t.Fatalf("damaged read: %s", got)
	}
}
