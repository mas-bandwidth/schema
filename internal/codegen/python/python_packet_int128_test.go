package python

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// int128Schema is the 128-bit kinds on their own (Raw, Ranged) and beside the
// core's kinds (Mixed: a bool, an enum, a byte, an if/else branch, a type by
// value, a union and a signed byte). Ranged covers each wire width class an
// int128 range lands in: one that fits int32, one that fits int64, one only
// 128 bits hold (±2^100, 102 wire bits), the whole int128 domain (128 bits), a
// specified default, and a degenerate range that costs zero bits.
const int128Schema = `package wide

enum Mode { Slow, Fast }

type Raw
{
    value uint128
}

type Ranged
{
    small  int128 | min = -5, max = 5
    mid    int128 | min = -5000000000, max = 5000000000
    big    int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    full   int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    bias   int128 = -250 | min = -1000, max = 1000
    locked int128 = -12345678901234 | min = -12345678901234, max = -12345678901234
}

union Pick
{
    raw    Raw
    ranged Ranged
}

type Mixed
{
    on   bool
    mode Mode
    head uint8
    if on
    {
        taken int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    }
    else
    {
        other uint128 = 36893488147419103232
    }
    raw  Raw
    pick Pick
    seed uint128 = 36893488147419103232
    tail int8
}
`

// int128Set is one field a case assigns: a dotted path from the root, the
// Go-side kind it converts through, and the value as a decimal (or true/false).
type int128Set struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type int128Case struct {
	Name  string      `json:"name"`
	Root  string      `json:"root"`
	Sets  []int128Set `json:"sets"`
	Reads []int128Set `json:"reads"`
	Hex   string      `json:"hex"`
}

const (
	pow100   = "1267650600228229401496703205376"
	pow127   = "170141183460469231731687303715884105728"
	pow127m1 = "170141183460469231731687303715884105727"
	pow128m1 = "340282366920938463463374607431768211455"
	pow64    = "18446744073709551616"
	lockedV  = "-12345678901234"
)

func s128(path, kind, value string) int128Set { return int128Set{path, kind, value} }

// rangedFields assigns every Ranged field under prefix: a union arm is written
// whole, so neither side leans on how its own storage holds an unselected arm.
func rangedFields(prefix, small, mid, big, full, bias string) []int128Set {
	return []int128Set{
		s128(prefix+"small", "i128", small),
		s128(prefix+"mid", "i128", mid),
		s128(prefix+"big", "i128", big),
		s128(prefix+"full", "i128", full),
		s128(prefix+"bias", "i128", bias),
		s128(prefix+"locked", "i128", lockedV),
	}
}

// int128Reads is every leaf each root's read must land, as the Go reference
// reads it back; a union's unselected arms are left out (each leg's storage
// keeps its own idea of them, SPEC §6).
func int128Reads(root string, sets []int128Set) []int128Set {
	ranged := func(prefix string) []int128Set {
		var out []int128Set
		for _, f := range []string{"small", "mid", "big", "full", "bias", "locked"} {
			out = append(out, s128(prefix+f, "i128", ""))
		}
		return out
	}
	switch root {
	case "Raw":
		return []int128Set{s128("value", "u128", "")}
	case "Ranged":
		return ranged("")
	}
	reads := []int128Set{
		s128("on", "bool", ""), s128("mode", "Mode", ""), s128("head", "u8", ""),
		s128("taken", "i128", ""), s128("other", "u128", ""), s128("raw.value", "u128", ""),
		s128("pick.type", "PickType", ""), s128("seed", "u128", ""), s128("tail", "i8", ""),
	}
	for _, s := range sets {
		if s.Path == "pick.type" && s.Value == "1" {
			reads = append(reads, s128("pick.raw.value", "u128", ""))
		}
		if s.Path == "pick.type" && s.Value == "2" {
			reads = append(reads, ranged("pick.ranged.")...)
		}
	}
	return reads
}

func int128Cases() []int128Case {
	cases := []int128Case{
		{Name: "raw-default", Root: "Raw"},
		{Name: "raw-one", Root: "Raw", Sets: []int128Set{s128("value", "u128", "1")}},
		{Name: "raw-max", Root: "Raw", Sets: []int128Set{s128("value", "u128", pow128m1)}},
		{Name: "raw-high-qword", Root: "Raw", Sets: []int128Set{s128("value", "u128", pow64)}},
		{Name: "raw-both-qwords", Root: "Raw", Sets: []int128Set{s128("value", "u128", "1512366075204170947332355369683137040")}},
		{Name: "ranged-default", Root: "Ranged"},
		{Name: "ranged-min", Root: "Ranged", Sets: rangedFields("", "-5", "-5000000000", "-"+pow100, "-"+pow127, "-1000")},
		{Name: "ranged-max", Root: "Ranged", Sets: rangedFields("", "5", "5000000000", pow100, pow127m1, "1000")},
		{Name: "ranged-middle", Root: "Ranged", Sets: rangedFields("", "0", "-1", "1", "-1", "0")},
		{Name: "mixed-default", Root: "Mixed"},
		{Name: "mixed-on", Root: "Mixed", Sets: append([]int128Set{
			s128("on", "bool", "true"), s128("mode", "Mode", "2"), s128("head", "u8", "255"),
			s128("taken", "i128", "1267650600228229401496703205369"), s128("raw.value", "u128", pow127),
			s128("pick.type", "PickType", "2"), s128("seed", "u128", pow128m1), s128("tail", "i8", "-128"),
		}, rangedFields("pick.ranged.", "-4", "4999999999", "-"+pow100, "-170141183460469231731687303715884105727", "999")...)},
		{Name: "mixed-off", Root: "Mixed", Sets: []int128Set{
			s128("on", "bool", "false"), s128("mode", "Mode", "1"), s128("head", "u8", "7"),
			s128("other", "u128", "12345678901234567890123456789"),
			s128("pick.type", "PickType", "1"), s128("pick.raw.value", "u128", "99"), s128("tail", "i8", "127"),
		}},
		{Name: "mixed-no-pick", Root: "Mixed", Sets: []int128Set{
			s128("on", "bool", "true"), s128("taken", "i128", "-"+pow100), s128("pick.type", "PickType", "0"),
		}},
	}
	for i := range cases {
		if cases[i].Sets == nil {
			cases[i].Sets = []int128Set{}
		}
		cases[i].Reads = int128Reads(cases[i].Root, cases[i].Sets)
	}
	return cases
}

// int128Forge is a hostile input the Go reference forges from one of its own
// valid packets, with the Go reader's verdict.
type int128Forge struct {
	Name    string `json:"name"`
	Root    string `json:"root"`
	Hex     string `json:"hex"`
	Verdict string `json:"verdict"`
}

func goField(path string) string {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, ".")
}

func goSetExpr(s int128Set) string {
	switch s.Kind {
	case "u128":
		return fmt.Sprintf("u128v(%q)", s.Value)
	case "i128":
		return fmt.Sprintf("i128v(%q)", s.Value)
	case "bool":
		return s.Value
	case "u8":
		return "uint8(" + s.Value + ")"
	case "i8":
		return "int8(" + s.Value + ")"
	}
	return s.Kind + "(" + s.Value + ")"
}

func goReadExpr(s int128Set) string {
	x := "got." + goField(s.Path)
	switch s.Kind {
	case "u128":
		return "decU(" + x + ")"
	case "i128":
		return "decI(" + x + ")"
	case "bool":
		return "decB(" + x + ")"
	}
	return "fmt.Sprint(int64(" + x + "))"
}

// int128GoOracle is the Go reference: each case built at its New* defaults,
// written, read back, every leaf printed as the Go reader lands it, and the
// hostile inputs forged from those packets with the Go reader's verdict.
func int128GoOracle(cases []int128Case, hasNew map[string]bool) string {
	var b strings.Builder
	b.WriteString(`package wide

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

var two128 = new(big.Int).Lsh(big.NewInt(1), 128)

func u128v(s string) serialize.Uint128 {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	n.Mod(n, two128)
	lo := new(big.Int).And(n, new(big.Int).SetUint64(^uint64(0))).Uint64()
	return serialize.Uint128{Lo: lo, Hi: new(big.Int).Rsh(n, 64).Uint64()}
}

func i128v(s string) serialize.Int128 {
	u := u128v(s)
	return serialize.Int128{Lo: u.Lo, Hi: u.Hi}
}

func decU(x serialize.Uint128) string {
	n := new(big.Int).Lsh(new(big.Int).SetUint64(x.Hi), 64)
	return n.Or(n, new(big.Int).SetUint64(x.Lo)).String()
}

func decI(x serialize.Int128) string {
	n := new(big.Int).Lsh(new(big.Int).SetUint64(x.Hi), 64)
	n.Or(n, new(big.Int).SetUint64(x.Lo))
	if x.Hi>>63 == 1 {
		n.Sub(n, two128)
	}
	return n.String()
}

func decB(x bool) string {
	if x {
		return "true"
	}
	return "false"
}

func verdict(err error) string {
	if err != nil {
		return "refused"
	}
	return "read"
}

func TestInt128Oracle(t *testing.T) {
	packets := map[string][]byte{}
`)
	for _, c := range cases {
		// New* exists only for a type with a specified default somewhere in it
		if hasNew[c.Root] {
			fmt.Fprintf(&b, "\t{\n\t\tvalue := New%s()\n", c.Root)
		} else {
			fmt.Fprintf(&b, "\t{\n\t\tvar value %s\n", c.Root)
		}
		for _, s := range c.Sets {
			fmt.Fprintf(&b, "\t\tvalue.%s = %s\n", goField(s.Path), goSetExpr(s))
		}
		fmt.Fprintf(&b, "\t\tbuf := make([]byte, %sMaxBytes)\n", c.Root)
		b.WriteString("\t\tws := serialize.NewWriteStream(buf)\n")
		fmt.Fprintf(&b, "\t\tif err := Write%s(ws, &value); err != nil {\n\t\t\tt.Fatalf(\"%s: %%v\", err)\n\t\t}\n", c.Root, c.Name)
		b.WriteString("\t\tws.Flush()\n\t\tdata := append([]byte(nil), ws.Data()...)\n")
		fmt.Fprintf(&b, "\t\tpackets[%q] = data\n", c.Name)
		fmt.Fprintf(&b, "\t\tfmt.Printf(\"CASE|%s|%%x\\n\", data)\n", c.Name)
		fmt.Fprintf(&b, "\t\tvar got %s\n", c.Root)
		fmt.Fprintf(&b, "\t\tif err := Read%s(serialize.NewReadStream(data), &got); err != nil {\n\t\t\tt.Fatalf(\"%s read: %%v\", err)\n\t\t}\n", c.Root, c.Name)
		for _, r := range c.Reads {
			fmt.Fprintf(&b, "\t\tfmt.Printf(\"READ|%s|%s|%%s\\n\", %s)\n", c.Name, r.Path, goReadExpr(r))
		}
		b.WriteString("\t}\n")
	}
	b.WriteString(`	forge := func(name, root, from string, edit func([]byte) []byte) {
		data := edit(append([]byte(nil), packets[from]...))
		var err error
		switch root {
		case "Raw":
			var v Raw
			err = ReadRaw(serialize.NewReadStream(data), &v)
		case "Ranged":
			var v Ranged
			err = ReadRanged(serialize.NewReadStream(data), &v)
		case "Mixed":
			var v Mixed
			err = ReadMixed(serialize.NewReadStream(data), &v)
		}
		fmt.Printf("FORGE|%s|%s|%x|%s\n", name, root, data, verdict(err))
	}
	setBits := func(lo, n int) func([]byte) []byte {
		return func(b []byte) []byte {
			for i := lo; i < lo+n; i++ {
				b[i/8] |= 1 << (i % 8)
			}
			return b
		}
	}
	// small is the first field: 4 bits of offset over a span of 10
	forge("ranged-small-over-span", "Ranged", "ranged-min", setBits(0, 4))
	// big follows small (4 bits) and mid (34 bits): 102 bits over a span of 2^101
	forge("ranged-big-over-span", "Ranged", "ranged-min", setBits(38, 102))
	// the packet's one padding bit, set
	forge("ranged-padding", "Ranged", "ranged-min", func(b []byte) []byte { b[len(b)-1] |= 0x80; return b })
	forge("raw-short", "Raw", "raw-max", func(b []byte) []byte { return b[:len(b)-1] })
	forge("raw-trailing", "Raw", "raw-max", func(b []byte) []byte { return append(b, 0) })
	forge("mixed-short", "Mixed", "mixed-on", func(b []byte) []byte { return b[:len(b)-1] })
	// taken follows on (1 bit), mode (2 bits) and head (8 bits)
	forge("mixed-taken-over-span", "Mixed", "mixed-no-pick", setBits(11, 102))
}
`)
	return b.String()
}

// runInt128Oracle generates the fixture's Go unit, runs the reference beside
// it, and returns the cases with their packets and every forged input.
func runInt128Oracle(t *testing.T, u *ir.Unit) ([]int128Case, []int128Forge) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain and the serialize.go runtime (the 128-bit reference)")
	cases := int128Cases()
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("the Go reference does not generate the fixture: %v", err)
	}
	dir := t.TempDir()
	files := map[string][]byte{}
	hasNew := map[string]bool{}
	for name, data := range packet {
		files[name] = data
		for _, c := range cases {
			if strings.Contains(string(data), "\nfunc New"+c.Root+"() ") {
				hasNew[c.Root] = true
			}
		}
	}
	files["go.mod"] = []byte(fmt.Sprintf("module wide\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n",
		slowtest.SerializeGo(t, repoRoot(t))))
	files["oracle_test.go"] = []byte(int128GoOracle(cases, hasNew))
	writeTree(t, dir, files)
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestInt128Oracle$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Go reference: %v\n%s", err, out)
	}
	byName := map[string]*int128Case{}
	for i := range cases {
		byName[cases[i].Name] = &cases[i]
		cases[i].Reads = nil
	}
	var forged []int128Forge
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "|")
		switch fields[0] {
		case "CASE":
			byName[fields[1]].Hex = fields[2]
		case "READ":
			c := byName[fields[1]]
			kind := ""
			for _, r := range int128Reads(c.Root, c.Sets) {
				if r.Path == fields[2] {
					kind = r.Kind
				}
			}
			c.Reads = append(c.Reads, s128(fields[2], kind, fields[3]))
		case "FORGE":
			forged = append(forged, int128Forge{Name: fields[1], Root: fields[2], Hex: fields[3], Verdict: fields[4]})
		}
	}
	for _, c := range cases {
		if _, err := hex.DecodeString(c.Hex); err != nil || len(c.Reads) == 0 {
			t.Fatalf("the Go reference gave case %s no packet or no reads:\n%s", c.Name, out)
		}
	}
	if len(forged) != 7 {
		t.Fatalf("the Go reference forged %d inputs, want 7:\n%s", len(forged), out)
	}
	for _, f := range forged {
		if f.Verdict != "refused" {
			t.Fatalf("the Go reference reads the forged %s; every forge must be one it refuses:\n%s", f.Name, out)
		}
	}
	return cases, forged
}

// int128Bad is a write the contract forbids: the value as a decimal, or one of
// the named non-int literals.
type int128Bad struct {
	Root  string `json:"root"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

func int128BadWrites() []int128Bad {
	var bad []int128Bad
	ranged := func(root, prefix string) {
		bounds := map[string][2]string{
			"small":  {"-6", "6"},
			"mid":    {"-5000000001", "5000000001"},
			"big":    {"-1267650600228229401496703205377", "1267650600228229401496703205377"},
			"full":   {"-170141183460469231731687303715884105729", pow127},
			"bias":   {"-1001", "1001"},
			"locked": {"-12345678901235", "-12345678901233"},
		}
		for _, f := range []string{"small", "mid", "big", "full", "bias", "locked"} {
			for _, v := range []string{bounds[f][0], bounds[f][1], "True", "1.5", "str", "None"} {
				bad = append(bad, int128Bad{root, prefix + f, v})
			}
		}
	}
	for _, v := range []string{"-1", "340282366920938463463374607431768211456", "True", "1.5", "str", "None"} {
		bad = append(bad, int128Bad{"Raw", "value", v})
		bad = append(bad, int128Bad{"Mixed", "seed", v})
		bad = append(bad, int128Bad{"Mixed", "raw.value", v})
	}
	ranged("Ranged", "")
	for _, v := range []string{"-1267650600228229401496703205377", "1267650600228229401496703205377", "False"} {
		bad = append(bad, int128Bad{"Mixed", "taken", v})
	}
	return bad
}

// int128Probe reads every case's Go packet and requires each leaf exactly as
// the Go reader landed it, writes it back byte-identical, builds the same value
// from its assignments and writes the Go packet, refuses every truncation, a
// trailing byte and every forged input, and refuses every out-of-contract
// write with InvalidValue.
const int128Probe = `
import json
import sys
from functools import reduce
import wide
from wide import InvalidValue, Refused

with open("cases.json") as f:
    spec = json.load(f)
fail = False


def report(msg):
    global fail
    fail = True
    print("FAILED: " + msg)


def parse(kind, text):
    if kind == "bool":
        return text == "true"
    return int(text)


def get(value, path):
    return reduce(getattr, path.split("."), value)


def put(value, path, x):
    parts = path.split(".")
    setattr(get(value, ".".join(parts[:-1])) if len(parts) > 1 else value, parts[-1], x)


def written(name, value):
    try:
        return value.write()
    except InvalidValue as exc:
        report(name + ": the write was refused: " + str(exc))
        return None


for case in spec["cases"]:
    cls = getattr(wide, case["root"])
    data = bytes.fromhex(case["hex"])
    try:
        got = cls.read(data)
    except Refused as exc:
        report(case["name"] + ": the Go packet was refused: " + exc.reason)
        continue
    for r in case["reads"]:
        want = parse(r["kind"], r["value"])
        have = get(got, r["path"])
        if type(have) is not type(want) or have != want:
            report("%s: %s read %r, the Go reference read %r" % (case["name"], r["path"], have, want))
    again = written(case["name"], got)
    if again is not None and again != data:
        report(case["name"] + ": the write-back differs from the Go packet: " + again.hex())
    built = cls()
    for s in case["sets"]:
        put(built, s["path"], parse(s["kind"], s["value"]))
    out = written(case["name"], built)
    if out is not None and out != data:
        report(case["name"] + ": the built value writes " + out.hex() + ", the Go reference wrote " + case["hex"])
    if out is not None and cls.read(out) != got:
        report(case["name"] + ": the built value does not read back equal")
    for k in range(len(data)):
        try:
            cls.read(data[:k])
        except Refused:
            pass
        else:
            report("%s: a %d-byte truncation was read" % (case["name"], k))
    try:
        cls.read(data + b"\x00")
    except Refused:
        pass
    else:
        report(case["name"] + ": a trailing byte was read")

for forge in spec["forged"]:
    cls = getattr(wide, forge["root"])
    try:
        cls.read(bytes.fromhex(forge["hex"]))
    except Refused:
        pass
    else:
        report(forge["name"] + ": read, the Go reference refused it")

literals = {"True": True, "False": False, "1.5": 1.5, "str": "1", "None": None}
for bad in spec["bad"]:
    value = getattr(wide, bad["root"])()
    if bad["path"] == "taken":
        value.on = True
    x =literals[bad["value"]] if bad["value"] in literals else int(bad["value"])
    put(value, bad["path"], x)
    try:
        value.write()
    except InvalidValue:
        pass
    else:
        report("%s.%s = %r was written" % (bad["root"], bad["path"], x))

if fail:
    sys.exit(1)
print("ok")
`

func int128Generate(t *testing.T) (*ir.Unit, map[string][]byte) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Wide.schema"), []byte(int128Schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the 128-bit fixture does not generate: %v", err)
	}
	return u, files
}

func int128Spec(t *testing.T, cases []int128Case, forged []int128Forge) []byte {
	t.Helper()
	spec, err := json.Marshal(map[string]any{"cases": cases, "forged": forged, "bad": int128BadWrites()})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// TestPythonPacketInt128 holds raw uint128 and ranged int128 (SPEC §4.3) to
// the Go reference on fixture schemas of their own, alone and beside the
// core's kinds, under both pinned interpreters and mypy --strict, and requires
// that no C++-pinned golden is blocked by a 128-bit integer any more: the
// examples128 goldens all declare fixed(I, F) as well, so they stay on the
// harness's skip list under that kind for the card that carries it.
func TestPythonPacketInt128(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, pin := range packetPins() {
		if pin.note != "" {
			continue
		}
		paths := make([]string, len(pin.paths))
		for i, p := range pin.paths {
			paths[i] = filepath.Join(root, filepath.FromSlash(p))
		}
		if blocker := rootBlocker(loadPythonUnit(t, paths...), pin.root); strings.HasPrefix(blocker, "int") {
			t.Errorf("golden %s is still blocked by %s", pin.name, blocker)
		}
	}
	u, files := int128Generate(t)
	text := string(files["wide/Wide.py"])
	for _, want := range []string{"serialize_uint128", "serialize_int128", "serialize_int64", "serialize_int("} {
		if !strings.Contains(text, want) {
			t.Errorf("the generated fixture never calls %s:\n%s", want, text)
		}
	}
	cases, forged := runInt128Oracle(t, u)
	spec := int128Spec(t, cases, forged)
	probe := pyrun.ImportPrelude + int128Probe
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "cases.json": spec})))
			if got != "ok" {
				t.Fatalf("the 128-bit kinds under %s:\n%s", run.name, got)
			}
		})
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, err := pyrun.Analyze(t, dir, u.Package)
	if err != nil || !strings.Contains(out, "Success: no issues found") {
		t.Fatalf("the 128-bit fixture under mypy --strict: %v\n%s", err, out)
	}
}

// TestPythonPacketInt128NegativeControl plants a fault in a copy of the
// generated Raw reader (its uint128 lands one too high) and requires the
// 128-bit gate to go red on the Go-reference read marker.
func TestPythonPacketInt128NegativeControl(t *testing.T) {
	t.Parallel()
	u, files := int128Generate(t)
	cases, forged := runInt128Oracle(t, u)
	module := "wide/Wide.py"
	text := string(files[module])
	at := strings.Index(text, "class Raw:")
	marker := "self.value = _s_ri.value"
	off := strings.Index(text[at:], marker)
	if at < 0 || off < 0 {
		t.Fatalf("the generated Raw reader has no uint128 assignment:\n%s", text)
	}
	off += at
	files[module] = []byte(text[:off] + "self.value = 1 + _s_ri.value" + text[off+len(marker):])
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py":    []byte(pyrun.ImportPrelude + int128Probe),
		"cases.json": int128Spec(t, cases, forged),
	})
	if err == nil || !strings.Contains(out, "FAILED: raw-default: value read 1, the Go reference read 0") {
		t.Fatalf("the planted Raw reader edit did not go red on the read marker (err %v):\n%s", err, out)
	}
}
