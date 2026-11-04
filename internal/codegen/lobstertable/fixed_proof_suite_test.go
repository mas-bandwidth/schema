package lobstertable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestLobsterFixedProofSuiteValues(t *testing.T) {
	fixedproof.RunValueSuite(t, lobsterFixedProofRunner{})
}

func TestLobsterFixedProofSuiteNegativeControl(t *testing.T) {
	fixedproof.RunNegativeControlSuite(t, lobsterFixedProofRunner{})
}

type lobsterFixedProofRunner struct{}

var _ fixedproof.LegRunner = lobsterFixedProofRunner{}

func (lobsterFixedProofRunner) Name() string { return "lobster" }

func (lobsterFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	out, err := lobsterProofRun(t, tc, u, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func (lobsterFixedProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	return lobsterProofRun(t, tc, u, wire, true, false)
}
func (lobsterFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	return lobsterProofRun(t, tc, u, nil, false, true)
}

func lobsterProofRun(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte, load, mutate bool) ([]byte, error) {
	t.Helper()
	files, err := lobster.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	if mutate {
		// Change the emitted record writer, leaving layout, hash and envelope intact.
		anchor := fmt.Sprintf("%sFixedPut(schema_rt.zeros(%d), 0, v)", tc.Root, tc.Body)
		replacement := "fixed_proof_mutate(" + anchor + ")"
		hits := 0
		for name, data := range files {
			hits += strings.Count(string(data), anchor)
			if strings.Contains(string(data), anchor) {
				files[name] = []byte(strings.ReplaceAll(string(data), anchor, replacement) + "\ndef fixed_proof_mutate(b:string) -> string:\n    return write_int8_le(b, 0, read_uint8_le(b, 0) ^ 1)\n")
			}
		}
		if hits != 1 {
			return nil, fmt.Errorf("%s: writer mutation anchors: %d", tc.ID, hits)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return nil, err
		}
	}
	setup, check := lobsterProofFixture(tc.ID)
	construct := fmt.Sprintf("let r:[probe.%s] = []\nfor(%d): push(r, probe.New%s())\n%s\n", tc.Root, tc.Count, tc.Root, setup)
	if load {
		construct = fmt.Sprintf(`let input = read_file("in.bin")
assert(input)
let r:[probe.%s] = []
for(%d): push(r, probe.New%s())
let report = schema_rt.TableReport {}
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let count = probe.%sFixedLoad(r, input, plan, report)
assert(count == %d)
%s
`, tc.Root, tc.Count, tc.Root, tc.Root, tc.Count, check)
	}
	main := "import .probeTable\n" + lobsterrun.HexProbe + construct + fmt.Sprintf(`let wire = probe.%sFixedSave(r)
assert(length(wire) == probe.%sFixedMeasure(%d))
hex(wire)
`, tc.Root, tc.Root, tc.Count)
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		return nil, err
	}
	out, code := lobsterrun.RunExit(t, dir, "main.lobster", map[string][]byte{"in.bin": wire})
	if code != 0 {
		return nil, fmt.Errorf("%s: native exit %d: %s", tc.ID, code, out)
	}
	return hex.DecodeString(strings.TrimSpace(string(out)))
}

// Each fixture authors native fields independently of the C++ oracle. Checks
// inspect the decoded fields before the reader is allowed to re-save them.
func lobsterProofFixture(id string) (setup, check string) {
	switch id {
	case "bool-values":
		return `r[1].A = true
r[1].B = false`, `assert(r[1].A == true)
assert(r[1].B == false)`
	case "signed-integers":
		return `r[1].A = 127
r[1].B = 32767
r[1].C = 2147483647
r[1].D = 9223372036854775807
r[2].A = -1
r[2].B = 0
r[2].C = 1
r[2].D = -2`, `assert(r[1].A == 127)
assert(r[1].B == 32767)
assert(r[1].C == 2147483647)
assert(r[1].D == 9223372036854775807)
assert(r[2].A == -1)
assert(r[2].B == 0)
assert(r[2].C == 1)
assert(r[2].D == -2)`
	case "unsigned-integers":
		return `r[1].A = 0
r[1].B = 0
r[1].C = 0
r[1].D = 0
r[2].A = 1
r[2].B = 256
r[2].C = 65536
r[2].D = 4294967296`, `assert(r[1].A == 0)
assert(r[1].B == 0)
assert(r[1].C == 0)
assert(r[1].D == 0)
assert(r[2].A == 1)
assert(r[2].B == 256)
assert(r[2].C == 65536)
assert(r[2].D == 4294967296)`
	case "signed-128":
		return `r[1].A = schema_rt.Int128 { lo: -1, hi: 0x7fffffffffffffff }
r[2].A = schema_rt.Int128 { lo: 1, hi: 0 }`, `assert(r[1].A.lo == -1)
assert(r[1].A.hi == 0x7fffffffffffffff)
assert(r[2].A.lo == 1)
assert(r[2].A.hi == 0)`
	case "unsigned-128":
		return `r[1].A = schema_rt.Int128 { lo: 0, hi: 0 }
r[2].A = schema_rt.Int128 { lo: 1, hi: 0 }`, `assert(r[1].A.lo == 0)
assert(r[1].A.hi == 0)
assert(r[2].A.lo == 1)
assert(r[2].A.hi == 0)`
	case "ranged-integers":
		return `r[0].A = -5
r[0].B = -1000000000000
r[0].C = 0
r[1].A = 5
r[1].B = 1000000000000
r[1].C = 1000`, `assert(r[0].A == -5)
assert(r[0].B == -1000000000000)
assert(r[0].C == 0)
assert(r[1].A == 5)
assert(r[1].B == 1000000000000)
assert(r[1].C == 1000)`
	case "bits-values":
		return `r[0].A = 7
r[0].B = 4095
r[0].C = 1048575
r[0].D = 1099511627775
r[1].A = 0
r[1].B = 0
r[1].C = 0
r[1].D = 0`, `assert(r[0].A == 7)
assert(r[0].B == 4095)
assert(r[0].C == 1048575)
assert(r[0].D == 1099511627775)
assert(r[1].A == 0)
assert(r[1].B == 0)
assert(r[1].C == 0)
assert(r[1].D == 0)`
	case "float32-values":
		return `r[1].A = -1024.25
r[1].B = 2048.75`, `assert(r[1].A == -1024.25)
assert(r[1].B == 2048.75)`
	case "float64-values":
		return `r[1].A = -987654321.125
r[1].B = 123456789.875`, `assert(r[1].A == -987654321.125)
assert(r[1].B == 123456789.875)`
	case "compressed-floats":
		return `r[0].A = 0.25
r[0].B = -0.5
r[1].A = 0.0
r[1].B = -1.0
r[2].A = 1.0
r[2].B = 1.0`, `assert(r[0].A == 0.25)
assert(r[0].B == -0.5)
assert(r[1].A == 0.0)
assert(r[1].B == -1.0)
assert(r[2].A == 1.0)
assert(r[2].B == 1.0)`
	case "signed-fixed-point":
		return `r[0].A = -(8 << 16)
r[0].B = -(100 << 16)
r[0].C = schema_rt.Int128 { lo: 0xffffffffff000000, hi: -1 }
r[1].A = (8 << 16)
r[1].B = (100 << 16)
r[1].C = schema_rt.Int128 { lo: 0x00fa0000, hi: 0 }`, `assert(r[0].A == -(8 << 16))
assert(r[0].B == -(100 << 16))
assert(r[0].C.lo == 0xffffffffff000000)
assert(r[0].C.hi == -1)
assert(r[1].A == (8 << 16))
assert(r[1].B == (100 << 16))
assert(r[1].C.lo == 0x00fa0000)
assert(r[1].C.hi == 0)`
	case "unsigned-fixed-point":
		return `r[0].A = 0
r[0].B = 0
r[0].C = schema_rt.Int128 { lo: 0, hi: 0 }
r[1].A = (16 << 16)
r[1].B = (100 << 16)
r[1].C = schema_rt.Int128 { lo: (1000 << 16), hi: 0 }`, `assert(r[0].A == 0)
assert(r[0].B == 0)
assert(r[0].C.lo == 0)
assert(r[0].C.hi == 0)
assert(r[1].A == (16 << 16))
assert(r[1].B == (100 << 16))
assert(r[1].C.lo == (1000 << 16))
assert(r[1].C.hi == 0)`
	case "flags-values":
		return `r[0].Caps = 0
r[1].Caps = 5
r[2].Caps = 7`, `assert(r[0].Caps == 0)
assert(r[1].Caps == 5)
assert(r[2].Caps == 7)`
	case "enum-values":
		return `r[0].G = probe.GradeNone
r[1].G = probe.GradeGold
r[2].G = probe.GradeBronze`, `assert(r[0].G == probe.GradeNone)
assert(r[1].G == probe.GradeGold)
assert(r[2].G == probe.GradeBronze)`
	case "utf8-values":
		return `r[1].S = "hello"
r[1].SLength = 5`, `assert(substring(r[1].S, 0, 5) == "hello")
assert(r[1].SLength == 5)`
	case "utf16-values":
		return `r[1].W = "\x41\x00\xb1\x03\x3d\xd8\x00\xde"
r[1].WLength = 4`, `assert(substring(r[1].W, 0, 8) == "\x41\x00\xb1\x03\x3d\xd8\x00\xde")
assert(r[1].WLength == 4)`
	case "byte-buffers":
		return `r[1].Blob = "\xff\x00\x5a\x01\x02\x03"
r[1].BlobLength = 6`, `assert(substring(r[1].Blob, 0, 6) == "\xff\x00\x5a\x01\x02\x03")
assert(r[1].BlobLength == 6)`
	case "nested-types":
		return `r[0].Badge.Title = "hi"
r[0].Badge.TitleLength = 2`, `assert(substring(r[0].Badge.Title, 0, 2) == "hi")
assert(r[0].Badge.TitleLength == 2)`
	case "nested-fixed-tables":
		return `r[0].Child.N = -2147483648
r[1].Child.N = 2147483647`, `assert(r[0].Child.N == -2147483648)
assert(r[1].Child.N == 2147483647)`
	case "fixed-arrays":
		return `r[0].Tag[0] = -2147483648
r[0].Tag[1] = 0
r[0].Tag[2] = 2147483647`, `assert(r[0].Tag[0] == -2147483648)
assert(r[0].Tag[1] == 0)
assert(r[0].Tag[2] == 2147483647)`
	case "counted-arrays":
		return `r[1].NumsCount = 3
r[1].Nums[0] = 10
r[1].Nums[1] = 65535
r[1].Nums[2] = 32768`, `assert(r[1].NumsCount == 3)
assert(r[1].Nums[0] == 10)
assert(r[1].Nums[1] == 65535)
assert(r[1].Nums[2] == 32768)`
	case "keyed-arrays":
		return `r[0].Slots[0] = -2147483648
r[0].Slots[1] = 2147483647`, `assert(r[0].Slots[0] == -2147483648)
assert(r[0].Slots[1] == 2147483647)`
	case "nested-keyed-arrays":
		return `r[0].Slots[0].N = 11
r[0].Slots[1].N = -22`, `assert(r[0].Slots[0].N == 11)
assert(r[0].Slots[1].N == -22)`
	case "union-values":
		return `r[0].Body.Type = probe.BodyTypeV
r[0].Body.V.X = 11
r[0].Body.V.Y = -22
r[1].Body.Type = probe.BodyTypeT
r[1].Body.T.N = 41
r[2].Body.Type = probe.BodyTypeN
r[2].Body.N = 1073741821`, `assert(r[0].Body.Type == probe.BodyTypeV)
assert(r[0].Body.V.X == 11)
assert(r[0].Body.V.Y == -22)
assert(r[1].Body.Type == probe.BodyTypeT)
assert(r[1].Body.T.N == 41)
assert(r[2].Body.Type == probe.BodyTypeN)
assert(r[2].Body.N == 1073741821)`
	case "union-field-arms":
		return `r[0].Body.Type = probe.BodyTypeSa
r[0].Body.Sa = -5
r[1].Body.Type = probe.BodyTypeTa
r[1].Body.Ta = "abc"
r[1].Body.TaLength = 3
r[2].Body.Type = probe.BodyTypeAa
r[2].Body.AaCount = 3
r[2].Body.Aa[0] = -1
r[2].Body.Aa[1] = 0
r[2].Body.Aa[2] = 1`, `assert(r[0].Body.Type == probe.BodyTypeSa)
assert(r[0].Body.Sa == -5)
assert(r[1].Body.Type == probe.BodyTypeTa)
assert(substring(r[1].Body.Ta, 0, 3) == "abc")
assert(r[1].Body.TaLength == 3)
assert(r[2].Body.Type == probe.BodyTypeAa)
assert(r[2].Body.AaCount == 3)
assert(r[2].Body.Aa[0] == -1)
assert(r[2].Body.Aa[1] == 0)
assert(r[2].Body.Aa[2] == 1)`
	case "payload-free-arms":
		return `r[0].Lead = 10
r[0].Pick.Type = probe.PickTypeAlpha
r[0].Pick.Alpha = 99
r[0].Trail = 20
r[1].Lead = 30
r[1].Pick.Type = probe.PickTypeQuiet
r[1].Trail = 40
r[2].Lead = 50
r[2].Pick.Type = probe.PickTypeSignal
r[2].Trail = 60`, `assert(r[0].Lead == 10)
assert(r[0].Pick.Type == probe.PickTypeAlpha)
assert(r[0].Pick.Alpha == 99)
assert(r[0].Trail == 20)
assert(r[1].Lead == 30)
assert(r[1].Pick.Type == probe.PickTypeQuiet)
assert(r[1].Trail == 40)
assert(r[2].Lead == 50)
assert(r[2].Pick.Type == probe.PickTypeSignal)
assert(r[2].Trail == 60)`
	case "union-arrays":
		return `r[0].PicksCount = 3
r[0].Picks[0].Type = probe.PickTypeA
r[0].Picks[0].A = 3
r[0].Picks[1].Type = probe.PickTypeB
r[0].Picks[1].B = -1099511627776
r[0].Picks[2].Type = probe.PickTypeA
r[0].Picks[2].A = 0`, `assert(r[0].PicksCount == 3)
assert(r[0].Picks[0].Type == probe.PickTypeA)
assert(r[0].Picks[0].A == 3)
assert(r[0].Picks[1].Type == probe.PickTypeB)
assert(r[0].Picks[1].B == -1099511627776)
assert(r[0].Picks[2].Type == probe.PickTypeA)
assert(r[0].Picks[2].A == 0)`
	case "optional-scalars":
		return `r[1].N = -123456
r[1].NPresent = true
r[1].G = probe.GradeSilver
r[1].GPresent = true`, `assert(r[1].N == -123456)
assert(r[1].NPresent == true)
assert(r[1].G == probe.GradeSilver)
assert(r[1].GPresent == true)`
	case "optional-nesting":
		return `r[1].P.X = 99
r[1].P.Y = -99
r[1].PPresent = true`, `assert(r[1].P.X == 99)
assert(r[1].P.Y == -99)
assert(r[1].PPresent == true)`
	case "optional-arrays":
		return `r[1].Xs[0] = 7
r[1].Xs[1] = -8
r[1].Xs[2] = 9
r[1].XsPresent = true
r[1].XsCount = 3`, `assert(r[1].Xs[0] == 7)
assert(r[1].Xs[1] == -8)
assert(r[1].Xs[2] == 9)
assert(r[1].XsPresent == true)
assert(r[1].XsCount == 3)`
	case "scalar-defaults":
		return `r[1].X = -9
r[1].G = probe.GradeBronze
r[1].F = -2.25`, `assert(r[1].X == -9)
assert(r[1].G == probe.GradeBronze)
assert(r[1].F == -2.25)`
	case "text-bytes-flags-defaults":
		return `r[1].S = "xyz"
r[1].SLength = 3
r[1].B = "\x00\xff\x5a\x01"
r[1].BLength = 4
r[1].C = 4`, `assert(substring(r[1].S, 0, 3) == "xyz")
assert(r[1].SLength == 3)
assert(substring(r[1].B, 0, 4) == "\x00\xff\x5a\x01")
assert(r[1].BLength == 4)
assert(r[1].C == 4)`
	case "record-measurement":
		return `r[0].A = 10
r[0].B = 20
r[1].A = 30
r[1].B = 40`, `assert(r[0].A == 10)
assert(r[0].B == 20)
assert(r[1].A == 30)
assert(r[1].B == 40)`
	case "fixed-file-roundtrip":
		return `r[1].A = 22
r[1].B = "test"
r[1].BLength = 4`, `assert(r[1].A == 22)
assert(substring(r[1].B, 0, 4) == "test")
assert(r[1].BLength == 4)`
	case "file-envelope":
		return `r[1].X = 10
r[1].Y = 20`, `assert(r[1].X == 10)
assert(r[1].Y == 20)`
	case "batch-capacity":
		return `r[1].X = 3
r[1].Y = 4
r[2].X = 5
r[2].Y = 6`, `assert(r[1].X == 3)
assert(r[1].Y == 4)
assert(r[2].X == 5)
assert(r[2].Y == 6)`
	case "plan-selection":
		return `r[1].X = 33
r[1].Y = 44`, `assert(r[1].X == 33)
assert(r[1].Y == 44)`
	case "compiled-plans":
		return `r[1].A = 100
r[1].B = -20
r[1].C = 55`, `assert(r[1].A == 100)
assert(r[1].B == -20)
assert(r[1].C == 55)`
	case "definition-hash":
		return `r[1].X = 9
r[1].B = "abc"
r[1].BLength = 3`, `assert(r[1].X == 9)
assert(substring(r[1].B, 0, 3) == "abc")
assert(r[1].BLength == 3)`
	case "fixed-closure":
		return `r[1].N.A = 30
r[1].N.B = 40
r[1].Z = 70`, `assert(r[1].N.A == 30)
assert(r[1].N.B == 40)
assert(r[1].Z == 70)`
	case "retirement":
		return `r[1].A = 10
r[1].B = 20
r[1].C = 30`, `assert(r[1].A == 10)
assert(r[1].B == 20)
assert(r[1].C == 30)`
	case "retired-runtime":
		return `r[1].X = 44
r[1].Y = 55
r[1].Z = 66`, `assert(r[1].X == 44)
assert(r[1].Y == 55)
assert(r[1].Z == 66)`
	case "optional-values":
		return `r[1].Plain.A = 11
r[1].Plain.B = 22
r[1].OptPresent = true
r[1].Opt.A = 33
r[1].Opt.B = 44`, `assert(r[1].Plain.A == 11)
assert(r[1].Plain.B == 22)
assert(r[1].OptPresent == true)
assert(r[1].Opt.A == 33)
assert(r[1].Opt.B == 44)`
	case "field-evolution":
		return `r[1].B = 100
r[1].C = 200`, `assert(r[1].B == 100)
assert(r[1].C == 200)`
	case "live-extents":
		return `r[1].ArrCount = 1
r[1].Arr[0] = probe.SmallOne
r[1].OptPresent = false
r[1].Tail = 88`, `assert(r[1].ArrCount == 1)
assert(r[1].Arr[0] == probe.SmallOne)
assert(r[1].OptPresent == false)
assert(r[1].Tail == 88)`
	case "writing":
		return `r[1].ArrCount = 1
r[1].Arr[0] = 0x1122
r[1].S = "A"
r[1].SLength = 1
r[1].B = "\x42"
r[1].BLength = 1
r[1].Pick.Type = probe.PickTypeNarrow
r[1].Pick.Narrow = 7`, `assert(r[1].ArrCount == 1)
assert(r[1].Arr[0] == 0x1122)
assert(substring(r[1].S, 0, 1) == "A")
assert(r[1].SLength == 1)
assert(substring(r[1].B, 0, 1) == "\x42")
assert(r[1].BLength == 1)
assert(r[1].Pick.Type == probe.PickTypeNarrow)
assert(r[1].Pick.Narrow == 7)`
	case "union-guards":
		return `r[1].U.Type = probe.OuterTypeNested
r[1].U.Nested.X = 12
r[1].U.Nested.Y = 0x12345678
r[1].Tail = 77`, `assert(r[1].U.Type == probe.OuterTypeNested)
assert(r[1].U.Nested.X == 12)
assert(r[1].U.Nested.Y == 0x12345678)
assert(r[1].Tail == 77)`
	case "plan-execution":
		return `r[1].A = 10
r[1].Sub.P = 30
r[1].Sub.Q = 40
r[1].XsCount = 2
r[1].Xs[0] = 50
r[1].Xs[1] = 60`, `assert(r[1].A == 10)
assert(r[1].Sub.P == 30)
assert(r[1].Sub.Q == 40)
assert(r[1].XsCount == 2)
assert(r[1].Xs[0] == 50)
assert(r[1].Xs[1] == 60)`
	case "interoperability":
		return `r[1].A = 42
r[1].S = "test"
r[1].SLength = 4`, `assert(r[1].A == 42)
assert(substring(r[1].S, 0, 4) == "test")
assert(r[1].SLength == 4)`
	default:
		panic("missing Lobster fixed proof fixture: " + id)
	}
}
