package moo

// THE PACKET-WIRE FUZZ DIFFERENTIAL (docs/PORT-MOO.md, THE LAYERS: layer 6).
// The moo codecs against the Go target's, over every packet corpus the leg
// generates (analyzeCorpora: examples, examples128 and test/packet-*), the
// way serialize.moo's fuzz/differential_test.go holds the runtime against
// serialize.go. For every round:
//
//   - WRITE: a random instance of a random packet type or union, drawn inside
//     every declared contract (fuzzvalue_test.go), is built in each language
//     and written; the two writers must agree on accept/refuse and emit
//     IDENTICAL bytes.
//   - READ: the Go bytes, and mutants of them (bit flips, byte overwrites,
//     truncation, trailing bytes, a random image), are read by each reader
//     followed by serializeEnd. The readers must agree on accept/refuse and on
//     the refusal reason as STANDARD.md spells it (a schema rule's refusal,
//     const, reserved, which no runtime latches, is "none" on both); an
//     accepted read is written back by each side and the two rewrites must be
//     identical, so the readers agree on every value the wire carries.
//
// A moo trap anywhere is a finding: hostile bytes never raise.
//
// It runs moo and builds Go, so it is behind SCHEMA_SLOW. The test leg runs one
// short seeded round (SCHEMA_MOO_FUZZ_N instances per unit, default 400); the
// nightly (make schema-moo-nightly-fuzz) runs SCHEMA_MOO_FUZZ_ROUNDS rounds from
// SCHEMA_MOO_FUZZ_SEED, a case count and never a clock, so every finding
// replays from its seed. SCHEMA_MOO_FUZZ_FAILURES names a directory each
// failing case is written to (the nightly uploads it).
//
// THE REVERSED WITNESSES: TestMooWireFuzzWriteNegativeControl plants a writer
// that drops the top bit of an 8-bit field, TestMooWireFuzzReadNegativeControl
// a union reader that takes its tag as raw bits (no bound), each in a scratch
// copy of the generated moo; the differential must go red on each, for that
// reason. No unit test here uses real time, a socket or the network.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fuzzDefaultSeed = 20261028

func envInt(t testing.TB, name string, def uint64) uint64 {
	t.Helper()
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		t.Fatalf("%s=%q: %v", name, s, err)
	}
	return v
}

// fuzzUnit is one corpus unit: its moo directory, its Go package, its roots.
type fuzzUnit struct {
	key   string // u0, u1, ...: the Go package directory and alias
	name  string // the corpus path, for messages
	unit  *ir.Unit
	dir   string // the generated moo
	roots []string
}

// fuzzCorpus generates every unit of analyzeCorpora in moo, and in Go into one
// module whose harness (fuzzHarness) is built once.
type fuzzCorpus struct {
	units []*fuzzUnit
	goBin string
}

func corpusUnits(t testing.TB) []*fuzzUnit {
	t.Helper()
	var out []*fuzzUnit
	for _, c := range analyzeCorpora {
		paths, _ := filepath.Glob(filepath.Join(repo(c...), "*.schema"))
		if len(paths) == 0 {
			t.Fatalf("no schema files under %s", filepath.Join(c...))
		}
		sort.Strings(paths)
		sets := [][]string{paths}
		if c[0] == "test" {
			sets = nil
			for _, p := range paths {
				sets = append(sets, []string{p})
			}
		}
		for _, s := range sets {
			name := filepath.Join(c...)
			if c[0] == "test" {
				name = filepath.Join(name, filepath.Base(s[0]))
			}
			u := unitFromFiles(t, s)
			out = append(out, &fuzzUnit{key: fmt.Sprintf("u%d", len(out)), name: name, unit: u, roots: roots(u)})
		}
	}
	return out
}

// buildCorpus generates the moo modules (patch, when not nil, rewrites them:
// the reversed witnesses) and builds the Go harness.
func buildCorpus(t *testing.T, patch func(t *testing.T, u *fuzzUnit, files map[string][]byte)) *fuzzCorpus {
	t.Helper()
	c := &fuzzCorpus{units: corpusUnits(t)}
	mod := t.TempDir()
	runtime := slowtest.SerializeGo(t, repo())
	gomod := fmt.Sprintf("module fuzzgo\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, u := range c.units {
		files, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("moo generate %s: %v", u.name, err)
		}
		if patch != nil {
			patch(t, u, files)
		}
		u.dir = t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(u.dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gofiles, err := golang.Generate(u.unit)
		if err != nil {
			t.Fatalf("go generate %s: %v", u.name, err)
		}
		gdir := filepath.Join(mod, "gen", u.key)
		if err := os.MkdirAll(gdir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range gofiles {
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			if err := os.WriteFile(filepath.Join(gdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(mod, "main.go"), []byte(c.harness(t, mod)), 0o644); err != nil {
		t.Fatal(err)
	}
	c.goBin = filepath.Join(mod, "fuzzgo")
	cmd := exec.Command("go", "build", "-o", c.goBin, ".")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the Go harness: %v\n%s", err, out)
	}
	return c
}

// harness is the Go side: the generic set/write/read program over a registry
// of every root of every unit.
func (c *fuzzCorpus) harness(t *testing.T, mod string) string {
	t.Helper()
	var reg, imports strings.Builder
	for _, u := range c.units {
		fmt.Fprintf(&imports, "\t%s \"fuzzgo/gen/%s\"\n", u.key, u.key)
		src := readGoSources(t, filepath.Join(mod, "gen", u.key))
		for _, r := range u.roots {
			typ := goname.Initialisms(r)
			fresh := fmt.Sprintf("func() any { var v %s.%s; return &v }", u.key, typ)
			if n := goname.Initialisms("New" + r); strings.Contains(src, "func "+n+"() "+typ+" {") {
				fresh = fmt.Sprintf("func() any { v := %s.%s(); return &v }", u.key, n)
			}
			fmt.Fprintf(&reg, "\t%q: {%s,\n\t\tfunc(s *serialize.WriteStream, v any) error { return %s.%s(s, v.(*%s.%s)) },\n\t\tfunc(s *serialize.ReadStream, v any) error { return %s.%s(s, v.(*%s.%s)) },\n\t\t%s.%s},\n",
				u.key+"."+r, fresh,
				u.key, goname.Initialisms("Write"+r), u.key, typ,
				u.key, goname.Initialisms("Read"+r), u.key, typ,
				u.key, goname.Initialisms(r+"MaxBytes"))
		}
	}
	return strings.Replace(strings.Replace(goHarness, "\t// IMPORTS\n", imports.String(), 1), "\t// REGISTRY\n", reg.String(), 1)
}

func readGoSources(t *testing.T, dir string) string {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var b strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

const goHarness = `// The Go side of the moo packet-wire fuzz differential
// (internal/codegen/moo/fuzz_test.go), generated by that test.
package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"

	serialize "github.com/mas-bandwidth/serialize.go/v2"

	// IMPORTS
)

type entry struct {
	fresh func() any
	write func(*serialize.WriteStream, any) error
	read  func(*serialize.ReadStream, any) error
	max   int
}

var registry = map[string]entry{
	// REGISTRY
}

func reason(err error) string {
	var r serialize.Refusal
	if errors.As(err, &r) {
		return r.String()
	}
	return "none"
}

var int128T = reflect.TypeOf(serialize.Int128{})
var uint128T = reflect.TypeOf(serialize.Uint128{})

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fuzzgo: "+format+"\n", args...)
	os.Exit(2)
}

func set(root reflect.Value, path, leaf string) {
	v := root
	for _, seg := range strings.Split(path, ".") {
		switch v.Kind() {
		case reflect.Struct:
			f := v.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, seg) })
			if !f.IsValid() {
				fail("%s: no field %q in %s", path, seg, v.Type())
			}
			v = f
		case reflect.Array, reflect.Slice:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= v.Len() {
				fail("%s: index %q out of %s", path, seg, v.Type())
			}
			v = v.Index(i)
		default:
			fail("%s: cannot descend into %s at %q", path, v.Type(), seg)
		}
	}
	kind, val, _ := strings.Cut(leaf, ":")
	switch kind {
	case "b":
		v.SetBool(val == "1")
	case "i":
		n, ok := new(big.Int).SetString(val, 10)
		if !ok {
			fail("%s: bad integer %q", path, val)
		}
		switch {
		case v.Type() == int128T || v.Type() == uint128T:
			m := new(big.Int).Mod(n, new(big.Int).Lsh(big.NewInt(1), 128))
			lo := new(big.Int).And(m, new(big.Int).SetUint64(math.MaxUint64)).Uint64()
			hi := new(big.Int).Rsh(m, 64).Uint64()
			v.FieldByName("Lo").SetUint(lo)
			v.FieldByName("Hi").SetUint(hi)
		case v.CanInt():
			if !n.IsInt64() || v.OverflowInt(n.Int64()) {
				fail("%s: %s overflows %s", path, val, v.Type())
			}
			v.SetInt(n.Int64())
		case v.CanUint():
			if !n.IsUint64() || v.OverflowUint(n.Uint64()) {
				fail("%s: %s overflows %s", path, val, v.Type())
			}
			v.SetUint(n.Uint64())
		default:
			fail("%s: an integer into %s", path, v.Type())
		}
	case "f32":
		b, err := strconv.ParseUint(val, 16, 32)
		if err != nil || v.Kind() != reflect.Float32 {
			fail("%s: f32 %q into %s", path, val, v.Type())
		}
		v.SetFloat(float64(math.Float32frombits(uint32(b))))
	case "f64":
		b, err := strconv.ParseUint(val, 16, 64)
		if err != nil || v.Kind() != reflect.Float64 {
			fail("%s: f64 %q into %s", path, val, v.Type())
		}
		v.SetFloat(math.Float64frombits(b))
	case "w":
		if len(val)%4 != 0 || v.Kind() != reflect.Array || len(val)/4 > v.Len() {
			fail("%s: wide units %q into %s", path, val, v.Type())
		}
		for i := 0; i < v.Len(); i++ {
			u := uint64(0)
			if 4*i < len(val) {
				x, err := strconv.ParseUint(val[4*i:4*i+4], 16, 16)
				if err != nil {
					fail("%s: bad unit %q", path, val[4*i:4*i+4])
				}
				u = x
			}
			v.Index(i).SetUint(u)
		}
	case "x":
		data, err := hex.DecodeString(val)
		if err != nil {
			fail("%s: bad hex %q", path, val)
		}
		switch v.Kind() {
		case reflect.String:
			v.SetString(string(data))
		case reflect.Array:
			if len(data) > v.Len() {
				fail("%s: %d bytes into %s", path, len(data), v.Type())
			}
			for i := 0; i < v.Len(); i++ {
				b := byte(0)
				if i < len(data) {
					b = data[i]
				}
				v.Index(i).SetUint(uint64(b))
			}
		case reflect.Slice:
			v.SetBytes(append([]byte(nil), data...))
		default:
			fail("%s: bytes into %s", path, v.Type())
		}
	default:
		fail("%s: unknown leaf %q", path, leaf)
	}
}

func write(e entry, v any) string {
	size := e.max
	if size < 8 {
		size = 8
	}
	ws := serialize.NewWriteStream(make([]byte, size))
	err := e.write(ws, v)
	if err == nil {
		ws.Flush()
		err = ws.Err()
	}
	if err != nil {
		return "R " + reason(err)
	}
	return "W " + hex.EncodeToString(ws.Data())
}

func lookup(name string) entry {
	e, ok := registry[name]
	if !ok {
		fail("no root %q", name)
	}
	return e
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<26)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	var cur entry
	var val any
	for in.Scan() {
		line := in.Text()
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "case": // case <root>: a fresh value of the root
			cur = lookup(f[1])
			val = cur.fresh()
		case "set": // set <path> <leaf>
			if len(f) == 3 {
				set(reflect.ValueOf(val).Elem(), f[1], f[2])
			} else {
				set(reflect.ValueOf(val).Elem(), f[1], "x:")
			}
		case "write":
			fmt.Fprintln(out, write(cur, val))
		case "read": // read <root> <hex|->: read, end, and rewrite when accepted
			e := lookup(f[1])
			data := []byte{}
			if f[2] != "-" {
				var err error
				if data, err = hex.DecodeString(f[2]); err != nil {
					fail("bad hex %q", f[2])
				}
			}
			v := e.fresh()
			rs := serialize.NewReadStream(data)
			err := e.read(rs, v)
			if err == nil {
				err = rs.SerializeEnd()
			}
			if err != nil {
				fmt.Fprintln(out, "F "+reason(err))
				continue
			}
			fmt.Fprintln(out, "A "+write(e, v))
		default:
			fail("unknown line %q", line)
		}
	}
	if err := in.Err(); err != nil {
		fail("%v", err)
	}
}
`

// mooPrelude is the moo side's shared code: hex, the guarded write, the
// guarded read. A trap prints RAISED and the line differs from Go's.
const mooPrelude = `local serialize = require("./serialize")
local function hexOf(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function write(mod: any, name: string, v: any): string
	local size = mod[name .. "MaxBytes"]
	if size < 8 then
		size = 8
	end
	local ws = serialize.WriteStream.new(buffer.create(size))
	local ok, res = pcall(mod["Write" .. name], ws, v)
	if not ok then
		return "RAISED " .. tostring(res)
	end
	if not res then
		return "R"
	end
	ws:flush()
	return "W " .. hexOf(ws:data())
end
local function read(mod: any, name: string, data: buffer): string
	local v = mod["New" .. name]()
	local rs = serialize.ReadStream.new(data)
	local ok, res = pcall(function()
		return mod["Read" .. name](rs, v) and rs:serializeEnd()
	end)
	if not ok then
		return "RAISED " .. tostring(res)
	end
	if not res then
		return "F " .. rs:refusal()
	end
	return "A " .. write(mod, name, v)
end
`

// fuzzCase is one drawn instance and the reads made of it.
type fuzzCase struct {
	u     *fuzzUnit
	root  string
	inst  instance
	goW   string // the Go write line
	mooW  string // the moo write line
	reads []fuzzRead
}

type fuzzRead struct {
	how       string
	data      []byte
	goR, mooR string
}

// mooModule is the require alias of the module that declares root.
func mooModule(u *ir.Unit, root string) string { return "_" + u.DeclFile[root] }

func mooRequires(u *ir.Unit) string {
	var b strings.Builder
	for _, f := range u.Files {
		fmt.Fprintf(&b, "local _%s = require(\"./%s\")\n", f.Base, f.Base)
	}
	return b.String()
}

// runGo feeds the Go harness its script and returns its output lines.
func (c *fuzzCorpus) runGo(t *testing.T, script string) []string {
	t.Helper()
	cmd := exec.Command(c.goBin)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the Go harness: %v\n%s", err, stderr.Bytes())
	}
	return splitLines(stdout.String())
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// mutate returns the reads of one written image: the image itself, then
// mutants of it, each named by how it was made.
func mutate(r *rand.Rand, img []byte) []fuzzRead {
	reads := []fuzzRead{{how: "as written", data: img}}
	clone := func() []byte { return append([]byte(nil), img...) }
	for range 3 {
		switch op := r.IntN(6); {
		case op == 0 && len(img) > 0: // flip 1-4 bits
			b := clone()
			n := 1 + r.IntN(4)
			for range n {
				bit := r.IntN(len(b) * 8)
				b[bit/8] ^= 1 << (bit % 8)
			}
			reads = append(reads, fuzzRead{how: fmt.Sprintf("%d bit flips", n), data: b})
		case op == 1 && len(img) > 0: // overwrite a byte
			b := clone()
			i := r.IntN(len(b))
			b[i] = byte(r.IntN(256))
			reads = append(reads, fuzzRead{how: fmt.Sprintf("byte %d overwritten", i), data: b})
		case op == 2 && len(img) > 0: // truncate
			n := r.IntN(len(img))
			reads = append(reads, fuzzRead{how: fmt.Sprintf("truncated to %d", n), data: clone()[:n]})
		case op == 3: // trailing bytes
			n := 1 + r.IntN(3)
			b := clone()
			for range n {
				b = append(b, byte(r.IntN(256)))
			}
			reads = append(reads, fuzzRead{how: fmt.Sprintf("%d trailing bytes", n), data: b})
		case op == 4: // a random image of about the same length
			b := make([]byte, r.IntN(len(img)+2))
			for i := range b {
				b[i] = byte(r.IntN(256))
			}
			reads = append(reads, fuzzRead{how: "random bytes", data: b})
		default: // the high bits of the last byte: padding
			if len(img) == 0 {
				continue
			}
			b := clone()
			b[len(b)-1] |= byte(0x80 >> r.IntN(8))
			reads = append(reads, fuzzRead{how: "a set bit in the last byte", data: b})
		}
	}
	return reads
}

// fuzzResult is a round's tally and its first findings.
type fuzzResult struct {
	cases, reads, accepted, refused int
	reasons, mooReasons             map[string]int
	rootsSeen                       map[string]bool
	findings                        []string
}

// round runs one seeded round of n instances per unit and returns what it saw.
func (c *fuzzCorpus) round(t *testing.T, seed uint64, n int) *fuzzResult {
	t.Helper()
	res := &fuzzResult{reasons: map[string]int{}, mooReasons: map[string]int{}, rootsSeen: map[string]bool{}}
	for ui, u := range c.units {
		r := rand.New(rand.NewPCG(seed, uint64(ui)))
		d := &drawer{t: t, r: r, u: u.unit}
		cases := make([]*fuzzCase, n)
		var goScript, mooW strings.Builder
		mooW.WriteString(mooPrelude + mooRequires(u.unit) + "local C = {}\n")
		for i := range cases {
			// every root once, then random roots
			root := u.roots[i%len(u.roots)]
			if i >= len(u.roots) {
				root = u.roots[r.IntN(len(u.roots))]
			}
			fc := &fuzzCase{u: u, root: root, inst: d.draw(root)}
			cases[i] = fc
			mod := mooModule(u.unit, root)
			fmt.Fprintf(&goScript, "case %s.%s\n%s\nwrite\n", u.key, root, strings.Join(fc.inst.set, "\n"))
			fmt.Fprintf(&mooW, "C[%d] = function(): string\n\tlocal v = %s.New%s()\n", i+1, mod, root)
			for _, s := range fc.inst.moo {
				mooW.WriteString("\t" + s + "\n")
			}
			fmt.Fprintf(&mooW, "\treturn write(%s, %q, v)\nend\n", mod, root)
		}
		mooW.WriteString("for i = 1, #C do\n\tlocal ok, line = pcall(C[i])\n\tprint(if ok then line else \"RAISED \" .. tostring(line))\nend\n")
		goLines := c.runGo(t, goScript.String())
		writeFile(t, u.dir, "fuzzw.moo", mooW.String())
		mooLines := splitLines(string(moorun.Run(t, u.dir, "fuzzw.moo", nil)))
		if len(goLines) != n || len(mooLines) != n {
			t.Fatalf("%s: %d Go and %d moo write lines for %d cases", u.name, len(goLines), len(mooLines), n)
		}
		// the reads: every Go image and its mutants
		inputs := map[string][]byte{}
		var goR, mooR strings.Builder
		mooR.WriteString(mooPrelude + mooRequires(u.unit) + "local inputs = require(\"./inputs\")\nlocal R = {\n")
		k := 0
		for i, fc := range cases {
			fc.goW, fc.mooW = goLines[i], mooLines[i]
			res.cases++
			res.rootsSeen[u.key+"."+fc.root] = true
			img, ok := strings.CutPrefix(fc.goW, "W ")
			if !ok {
				continue
			}
			data, err := hex.DecodeString(img)
			if err != nil {
				t.Fatal(err)
			}
			fc.reads = mutate(r, data)
			for j := range fc.reads {
				k++
				name := "i" + strconv.Itoa(k)
				inputs[name] = fc.reads[j].data
				h := hex.EncodeToString(fc.reads[j].data)
				if h == "" {
					h = "-"
				}
				fmt.Fprintf(&goR, "read %s.%s %s\n", u.key, fc.root, h)
				fmt.Fprintf(&mooR, "\t{ %s, %q, %q },\n", mooModule(u.unit, fc.root), fc.root, name)
			}
		}
		mooR.WriteString("}\nfor _, c in R do\n\tprint(read(c[1], c[2], inputs[c[3]]))\nend\n")
		goRL := c.runGo(t, goR.String())
		writeFile(t, u.dir, "fuzzr.moo", mooR.String())
		mooRL := splitLines(string(moorun.Run(t, u.dir, "fuzzr.moo", inputs)))
		if len(goRL) != k || len(mooRL) != k {
			t.Fatalf("%s: %d Go and %d moo read lines for %d reads", u.name, len(goRL), len(mooRL), k)
		}
		k = 0
		for _, fc := range cases {
			for j := range fc.reads {
				fc.reads[j].goR, fc.reads[j].mooR = goRL[k], mooRL[k]
				k++
			}
			c.judge(res, seed, fc)
		}
	}
	return res
}

// judge compares one case's lines and records a finding when they differ.
func (c *fuzzCorpus) judge(res *fuzzResult, seed uint64, fc *fuzzCase) {
	var why []string
	goW, mooW := fc.goW, fc.mooW
	// a write refusal's reason is Go's alone: moo's guard refuses unlatched
	if strings.HasPrefix(goW, "R") {
		goW = "R"
	}
	if goW != mooW {
		why = append(why, fmt.Sprintf("write: Go %q, moo %q", fc.goW, fc.mooW))
	}
	for _, rd := range fc.reads {
		res.reads++
		if strings.HasPrefix(rd.goR, "F ") {
			res.refused++
			res.reasons[rd.goR[2:]]++
		}
		if strings.HasPrefix(rd.mooR, "F ") {
			res.mooReasons[rd.mooR[2:]]++
		} else {
			res.accepted++
		}
		goR, mooR := rd.goR, rd.mooR
		if strings.HasPrefix(goR, "A R") {
			goR = "A R"
		}
		// a schema rule's refusal ("none": Go's ErrValidation, moo's unlatched
		// false) names no runtime reason, so the other side's reason is not
		// comparable to it: Go folds its UTF-8, zero-byte and headroom checks
		// into ErrValidation where moo latches the runtime's reason, and a Go
		// flat run reads its bits before checking a const the moo reader
		// checks field by field. The verdict still must agree.
		//
		// The same flat run makes past_end one-sided: Go reads a run of fields
		// in one call, so a short image is past_end before any of the run's
		// values is checked, where the moo reader refuses the first bad value
		// it meets. Go's value_out_of_range, by contrast, means the whole run
		// was read, so moo (field by field over the same bits) must name it too.
		if goR == "F none" && strings.HasPrefix(mooR, "F ") || mooR == "F none" && strings.HasPrefix(goR, "F ") ||
			goR == "F past_end" && strings.HasPrefix(mooR, "F ") {
			goR, mooR = "F", "F"
		}
		if goR != mooR {
			why = append(why, fmt.Sprintf("read (%s) %x: Go %q, moo %q", rd.how, rd.data, rd.goR, rd.mooR))
		}
	}
	if len(why) == 0 {
		return
	}
	f := fmt.Sprintf("seed %d, %s, %s:\n  %s\n  moo value:\n    %s", seed, fc.u.name, fc.root,
		strings.Join(why, "\n  "), strings.Join(fc.inst.moo, "\n    "))
	res.findings = append(res.findings, f)
	if dir := os.Getenv("SCHEMA_MOO_FUZZ_FAILURES"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		name := filepath.Join(dir, fmt.Sprintf("seed-%d-%s-%s-%d.txt", seed, fc.u.key, fc.root, len(res.findings)))
		_ = os.WriteFile(name, []byte(f+"\n  go set lines:\n    "+strings.Join(fc.inst.set, "\n    ")+"\n"), 0o644)
	}
}

func reportFindings(t *testing.T, res *fuzzResult) {
	t.Helper()
	for i, f := range res.findings {
		if i == 10 {
			t.Errorf("... and %d more findings", len(res.findings)-10)
			break
		}
		t.Errorf("FINDING %s", f)
	}
}

// TestMooWireFuzzAgreesWithTheGoTarget is the differential: seeded rounds over
// every packet corpus, every root drawn at least once per unit per round, the
// reads non-vacuous (both verdicts seen, the runtime's refusals reached).
func TestMooWireFuzzAgreesWithTheGoTarget(t *testing.T) {
	slowtest.Gate(t, "moo and the Go toolchain (it runs the generated moo and builds the Go target)")
	t.Parallel()
	seed := envInt(t, "SCHEMA_MOO_FUZZ_SEED", fuzzDefaultSeed)
	n := int(envInt(t, "SCHEMA_MOO_FUZZ_N", 400))
	rounds := envInt(t, "SCHEMA_MOO_FUZZ_ROUNDS", 1)
	c := buildCorpus(t, nil)
	total := &fuzzResult{reasons: map[string]int{}, mooReasons: map[string]int{}, rootsSeen: map[string]bool{}}
	for k := range rounds {
		res := c.round(t, seed+k, n)
		reportFindings(t, res)
		total.cases += res.cases
		total.reads += res.reads
		total.accepted += res.accepted
		total.refused += res.refused
		for r, v := range res.reasons {
			total.reasons[r] += v
		}
		for r, v := range res.mooReasons {
			total.mooReasons[r] += v
		}
		for r := range res.rootsSeen {
			total.rootsSeen[r] = true
		}
		if len(res.findings) > 0 {
			t.Fatalf("seed %d: %d findings; replay with SCHEMA_MOO_FUZZ_SEED=%d SCHEMA_MOO_FUZZ_ROUNDS=1 SCHEMA_MOO_FUZZ_N=%d", seed+k, len(res.findings), seed+k, n)
		}
	}
	want := 0
	for _, u := range c.units {
		want += len(u.roots)
	}
	if len(total.rootsSeen) != want {
		t.Errorf("vacuous: %d of %d roots drawn", len(total.rootsSeen), want)
	}
	if total.accepted == 0 || total.refused == 0 {
		t.Errorf("vacuous: %d reads accepted, %d refused", total.accepted, total.refused)
	}
	// every refusal the packet corpora can reach, met by the moo reader: the
	// runtime's six and a schema rule's (const, reserved, a wstring rule)
	for _, r := range []string{"past_end", "value_out_of_range", "trailing_data", "nonzero_padding",
		"non_finite_float", "invalid_utf8", "embedded_nul", "none"} {
		if total.mooReasons[r] == 0 {
			t.Errorf("vacuous: no moo read refused %s", r)
		}
	}
	var rs []string
	for r, v := range total.mooReasons {
		rs = append(rs, fmt.Sprintf("%s=%d", r, v))
	}
	sort.Strings(rs)
	t.Logf("seed %d, %d rounds: %d instances over %d roots in %d units, %d reads (%d accepted, %d refused, moo reasons %s), no divergence",
		seed, rounds, total.cases, want, len(c.units), total.reads, total.accepted, total.refused, strings.Join(rs, " "))
}

// plant rewrites exactly one anchor in the named module of the named unit.
func plant(unitName, module, anchor, with string) func(t *testing.T, u *fuzzUnit, files map[string][]byte) {
	return func(t *testing.T, u *fuzzUnit, files map[string][]byte) {
		if u.name != unitName {
			return
		}
		src := string(files[module])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("reversed witness: the anchor matched %d times in %s — the emitter moved", strings.Count(src, anchor), module)
		}
		files[module] = []byte(strings.Replace(src, anchor, with, 1))
	}
}

// witness runs one round against a planted moo emitter bug and requires a
// finding on the planted root, of the planted kind.
func witness(t *testing.T, patch func(*testing.T, *fuzzUnit, map[string][]byte), root, kind string) {
	// root names the planted function; the finding may surface on any root
	// that carries it (Handle rides inside other types too)
	t.Helper()
	c := buildCorpus(t, patch)
	// a witness round is wider than the leg's: the planted bug must be met by
	// a drawn value or a mutant, and 2000 instances a unit is about a second
	res := c.round(t, envInt(t, "SCHEMA_MOO_FUZZ_SEED", fuzzDefaultSeed), 2000)
	for _, f := range res.findings {
		if strings.Contains(f, "\n  "+kind) {
			t.Logf("the planted bug was found:\n%s", f)
			return
		}
	}
	t.Fatalf("reversed witness: the differential stayed green on the planted %s bug in %s (%d findings: %v)", kind, root, len(res.findings), res.findings)
}

// TestMooWireFuzzWriteNegativeControl: a writer that drops the top bit of
// Handle.object_sequence (an 8-bit field the goldens only ever fill below 128)
// must make the write half of the differential go red on Handle.
func TestMooWireFuzzWriteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo and the Go toolchain (it runs the generated moo and builds the Go target)")
	t.Parallel()
	witness(t, plant("examples", "Types.moo",
		"\tNUM.value = value.ObjectSequence\n",
		"\tNUM.value = bit32.band(value.ObjectSequence, 0x7f) -- SABOTAGED\n"), "Handle", "write:")
}

// TestMooWireFuzzReadNegativeControl: a union reader that takes ProbeShape's
// tag as two raw bits, its bound dropped, accepts tag 3 where Go refuses
// value_out_of_range; the read half must go red on ProbeShape.
func TestMooWireFuzzReadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo and the Go toolchain (it runs the generated moo and builds the Go target)")
	t.Parallel()
	witness(t, plant("examples", "Wire.moo",
		"function M.ReadProbeShape(stream: any, value: ProbeShape): boolean\n\tif not stream:serializeInt(NUM, 0, 2) then",
		"function M.ReadProbeShape(stream: any, value: ProbeShape): boolean\n\tif not stream:serializeBits(NUM, 2) then -- SABOTAGED"), "ProbeShape", "read")
}
