package gotable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// fixedUnionTables is a fixed table whose closure reaches a union through a
// direct field and through a counted array of it, with one arm of every shape
// the fixed form lays out: a nested type with a ranged field, a string, a
// counted array, a fixed array, a float and a payload-free arm.
const fixedUnionTables = `package probe

type Small
{
    n int32 = 0 | min = 0, max = 100
}

union Pick
{
    small Small
    label string(6)
    nums  [..3]int16
    pair  [2]uint8
    ratio float32
    off
}

fixed table Holder
{
    head  int32 = 0
    pick  Pick
    picks [..2]Pick
    tail  int32 = 0
}
`

// fixedUnionArmsPlain and fixedUnionArmsRegional set and read the arms, once
// over the plain unit's union (one member per arm) and once over the regional
// unit's overlay Row (arms behind accessors). The probe is the same source in
// both units.
const fixedUnionArmsPlain = `
func setSmall(u *Pick, n int32)   { u.Type = PickTypeSmall; u.Small.N = n }
func setLabel(u *Pick, s string)  { u.Type = PickTypeLabel; u.LabelLength = int32(copy(u.Label[:], s)) }
func setLabelLength(u *Pick, n int32) { u.LabelLength = n }
func setNums(u *Pick, xs ...int16) { u.Type = PickTypeNums; u.NumsCount = int32(copy(u.Nums[:], xs)) }
func setNumsCount(u *Pick, n int32) { u.NumsCount = n }
func setPair(u *Pick, a, b uint8) { u.Type = PickTypePair; u.Pair = [2]uint8{a, b} }
func setRatio(u *Pick, r float32) { u.Type = PickTypeRatio; u.Ratio = r }
func getSmall(u *Pick) int32      { return u.Small.N }
func getLabel(u *Pick) string     { return string(u.Label[:u.LabelLength]) }
func getNums(u *Pick) []int16     { return u.Nums[:u.NumsCount] }
`

const fixedUnionArmsRegional = `
func setSmall(u *Pick, n int32)   { u.Type = PickTypeSmall; u.Small().Value.N = n }
func setLabel(u *Pick, s string)  { u.Type = PickTypeLabel; a := u.Label(); a.ValueLength = int32(copy(a.Value[:6], s)) }
func setLabelLength(u *Pick, n int32) { u.Label().ValueLength = n }
func setNums(u *Pick, xs ...int16) { u.Type = PickTypeNums; a := u.Nums(); a.ValueCount = int32(copy(a.Value[:], xs)) }
func setNumsCount(u *Pick, n int32) { u.Nums().ValueCount = n }
func setPair(u *Pick, a, b uint8) { u.Type = PickTypePair; u.Pair().Value = [2]uint8{a, b} }
func setRatio(u *Pick, r float32) { u.Type = PickTypeRatio; u.Ratio().Value = r }
func getSmall(u *Pick) int32      { return u.Small().Value.N }
func getLabel(u *Pick) string     { a := u.Label(); return string(a.Value[:a.ValueLength]) }
func getNums(u *Pick) []int16     { a := u.Nums(); return a.Value[:a.ValueCount] }
`

// fixedUnionProbe saves three records, one per arm shape at the root and two
// more in the counted array, to bytes pinned to the plain unit's writer; loads
// them back equal; and holds the write-side contract: a value off its range
// inside an arm, a text length past its N, a count past its Max and a tag past
// the union's arms each refuse the whole batch with -1 and
// TableRefuseInvalidValue, leaving the buffer untouched.
const fixedUnionProbe = `package probe

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

const pinned = "PINNED"

// engine is the engine's form-3 write of sample()[0] (tablewire.Encode).
const engine = "ENGINE"

func sample() []Holder {
	values := make([]Holder, 3)
	for k := range values {
		HolderReset(&values[k])
		values[k].Head = int32(10 + k)
		values[k].Tail = int32(-1 - k)
	}
	setSmall(&values[0].Pick, 42)
	setLabel(&values[0].Picks[0], "rowan")
	setNums(&values[0].Picks[1], 7, -8)
	values[0].PicksCount = 2
	setNums(&values[1].Pick, 1, 2, 3)
	setPair(&values[1].Picks[0], 4, 5)
	values[1].PicksCount = 1
	setRatio(&values[2].Pick, 0.5)
	return values
}

func TestFixedUnionRoundTrip(t *testing.T) {
	values := sample()
	buf := make([]byte, HolderFixedMeasure(int64(len(values))))
	if n := HolderFixedSave(values, buf); n != int64(len(buf)) {
		t.Fatalf("save: %d of %d", n, len(buf))
	}
	if got := hex.EncodeToString(buf); got != pinned {
		t.Fatalf("bytes differ from the plain unit's:\n got %s\nwant %s", got, pinned)
	}
	one := make([]byte, HolderFixedMeasure(1))
	if HolderFixedSave(values[:1], one) != int64(len(one)) || hex.EncodeToString(one) != engine {
		t.Fatalf("one record differs from the engine's write:\n got %x\nwant %s", one, engine)
	}
	back := make([]Holder, 3)
	var r TableReport
	if n := HolderFixedLoad(back, buf, make([]TableFixedEntry, 512), &r); n != 3 {
		t.Fatalf("load: %d %+v", n, r)
	}
	if back[0].Pick.Type != PickTypeSmall || getSmall(&back[0].Pick) != 42 || getLabel(&back[0].Picks[0]) != "rowan" ||
		len(getNums(&back[0].Picks[1])) != 2 || getNums(&back[0].Picks[1])[1] != -8 || back[2].Pick.Type != PickTypeRatio || back[1].Tail != -2 {
		t.Fatalf("load did not hand back the saved values: %+v", back)
	}
	again := make([]byte, len(buf))
	if HolderFixedSave(back, again) != int64(len(again)) || !bytes.Equal(again, buf) {
		t.Fatal("the loaded values do not save to the same bytes")
	}
}

func TestFixedUnionWriteRefuses(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(v []Holder)
	}{
		{"small.n=101", func(v []Holder) { setSmall(&v[2].Pick, 101) }},
		{"small.n=-1 in picks", func(v []Holder) { setSmall(&v[1].Picks[0], -1) }},
		{"label length 7", func(v []Holder) { setLabelLength(&v[0].Picks[0], 7) }},
		{"nums count 4", func(v []Holder) { setNumsCount(&v[1].Pick, 4) }},
		{"tag past the arms", func(v []Holder) { v[2].Pick.Type = PickTypeMax + 1 }},
	}
	for _, c := range cases {
		values := sample()
		c.spoil(values)
		buf := make([]byte, HolderFixedMeasure(int64(len(values))))
		for i := range buf {
			buf[i] = 0xa5
		}
		if n := HolderFixedSave(values, buf); n != -1 {
			t.Errorf("%s: the write was not refused: %d", c.name, n)
			continue
		}
		if bytes.Count(buf, []byte{0xa5}) != len(buf) {
			t.Errorf("%s: a refused write stored bytes", c.name)
		}
		if _, err := HolderFixedSaveReason(values, buf); !errors.Is(err, TableRefuseInvalidValue) {
			t.Errorf("%s: the reason is %v, not invalid_value", c.name, err)
		}
	}
}
`

// fixedUnionPinned is the plain unit's HolderFixedSave of sample(): the bytes
// the regional unit's overlay Row must write too.
const fixedUnionPinned = "03000000000000001e9c12daafac43999c010000180000002f45ad4ec426f5a10d2d00000004000000030c9a5fcc128f0a040400000000000000d8bb13c50dc40bbf0f0b000000060000008c9998e072e16c7d0d040000000100000071f801864ce363af0404000000000000003d62cb8fecfcf7390c0a00000000000000a0fd8452bacb9f5b0e0a000000010000000000000000000000030200000000000000319a88eb0d2569030e02000000010000000000000000000000060100000000000000e4ad77811124c9fb0a0400000000000000eabde221199b241a20000000000000000091e387e065ceb9b40e1a0000000100000000000000000000000f0b000000060000008c9998e072e16c7d0d040000000100000071f801864ce363af0404000000000000003d62cb8fecfcf7390c0a00000000000000a0fd8452bacb9f5b0e0a000000010000000000000000000000030200000000000000319a88eb0d2569030e02000000010000000000000000000000060100000000000000e4ad77811124c9fb0a0400000000000000eabde221199b241a20000000000000000023d79807ef564cd90404000000000000001e9c12daafac43990a000000012a000000000000000000020000000205000000726f77616e0003020000000700f8ff0000ffffffff1e9c12daafac43990b00000003030000000100020003000100000004040500000000000000000000000000000000000000feffffff1e9c12daafac43990c000000050000003f0000000000000000000000000000000000000000000000000000000000000000fdffffff"

// fixedUnionFirstText is sample()[0] in the text form, for the engine's write.
const fixedUnionFirstText = `{"head":10,"pick":{"small":{"n":42}},"picks":[{"label":"rowan"},{"nums":[7,-8]}],"tail":-1}`

func fixedUnionProbeFor(t *testing.T, arms string) string {
	t.Helper()
	m := tabletext.NewModel(unitOf(t, fixedUnionTables))
	inst := m.New(m.Lookup("Holder"))
	var r tabletext.Report
	if !m.Read(inst, []byte(fixedUnionFirstText), &r) {
		t.Fatalf("the engine did not place the first record: %+v", r)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(fixedUnionProbe, `const pinned = "PINNED"`, "const pinned = \""+fixedUnionPinned+"\"", 1)
	return strings.Replace(src, `const engine = "ENGINE"`, "const engine = \""+hex.EncodeToString(wire)+"\"", 1) + arms
}

// TestGoRegionalUnitUnionCarriesFixedForm holds §3.4 for a union in a regional
// unit: the fixed table that reaches a union, directly and through a counted
// array, emits the fixed form beside a variable table as it does alone; the
// overlay Row writes the plain unit's bytes, one record of them the engine's
// form-3 write byte for byte, and reads them back; and the
// writer refuses an out-of-range value, a text length, a count and a tag in
// every build, with -1 and invalid_value.
func TestGoRegionalUnitUnionCarriesFixedForm(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it builds and runs the generated unit under ps sampling)")
	t.Parallel()
	memceiling.Guard(t, memceiling.Default)
	plain, mixed := fixedUnionTables, fixedUnionTables+mixedFixedVariable
	mixedSrc := mixedFixedSource(t, mixed)
	if !strings.Contains(mixedSrc, "type Pick = PickRow") {
		t.Fatal("the mixed unit no longer stores Pick as its overlay Row; the test does not cover the Row")
	}
	for _, fn := range mixedFixedSurface {
		if decl := "func Holder" + fn + "("; !strings.Contains(mixedSrc, decl) {
			t.Errorf("mixed unit: %s is missing", decl)
		}
	}
	if t.Failed() {
		return
	}
	runGenerated(t, plain, fixedUnionProbeFor(t, fixedUnionArmsPlain))
	runGenerated(t, mixed, fixedUnionProbeFor(t, fixedUnionArmsRegional))
}

// TestGoRegionalUnionWriteRefusalNegativeControl removes the write-side
// contract from the regional unit's HolderFixedSave and requires the refusal
// probe to go red on its own words: the refusals come from that check.
func TestGoRegionalUnionWriteRefusalNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it builds and runs the generated unit under ps sampling)")
	t.Parallel()
	memceiling.Guard(t, memceiling.Default)
	const check = "if !HolderFixedCheckBody(&values[k]) {"
	edited := false
	out, err := runGeneratedEdited(t, fixedUnionTables+mixedFixedVariable, fixedUnionProbeFor(t, fixedUnionArmsRegional), func(files map[string][]byte) {
		for name, data := range files {
			if strings.Contains(string(data), check) {
				files[name] = []byte(strings.Replace(string(data), check, "if false && !HolderFixedCheckBody(&values[k]) {", 1))
				edited = true
			}
		}
	}, "-run", "^TestFixedUnionWriteRefuses$")
	if !edited {
		t.Fatal("negative control: the write-side check is not where this test removes it")
	}
	if err == nil {
		t.Fatalf("negative control: the write-side check is gone and the refusal probe stayed green\n%s", out)
	}
	for _, name := range []string{"small.n=101", "small.n=-1 in picks", "label length 7", "nums count 4", "tag past the arms"} {
		if !strings.Contains(string(out), name+": the write was not refused") {
			t.Errorf("negative control: %q did not go red on its own words\n%s", name, out)
		}
	}
}

// TestGoTabledemoFixedRootsWithUnionsCarryFixedForm: in the examples unit,
// which holds variable tables, RootConfig and LoadoutConfig (whose closure
// reaches the Effect union through WeaponConfig) carry the fixed form's writer
// and its write-side contract. The fuzz unit is that unit as the compiler
// emits it (TestGoFuzzUnitIsCurrent).
func TestGoTabledemoFixedRootsWithUnionsCarryFixedForm(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("fuzzunit", "TablesTable.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"RootConfig", "LoadoutConfig"} {
		for _, decl := range []string{"func " + table + "FixedSave(", "func " + table + "FixedSaveReason(", "func " + table + "FixedLoad(", "if !" + table + "FixedCheckBody(&values[k]) {"} {
			if !strings.Contains(string(src), decl) {
				t.Errorf("%s: %q is missing", table, decl)
			}
		}
	}
}
