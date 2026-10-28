package luatable

// A PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE, IN EVERY FORM
// (docs/SPEC-TABLES.md §3.3, "A PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE";
// §16.3 for the text form). A float32 or float64 with no declared range rides
// every form as its bit pattern (§3). The rule settles the two places the
// forms could still disagree:
//
//   - -0 IS A VALUE: the default elision compares BIT PATTERNS, so a -0 under
//     a +0 default rides the variable and message forms instead of being
//     elided and read back as +0, and the text reads "-0" as -0.
//   - a NaN or an infinity rides the binary forms bit for bit; the JSON writer
//     has no spelling for one and refuses.
//
// The generated Go of the same unit is the oracle for the saved bytes, as in
// internal/codegen/gotable/floatspecial_test.go. Every assertion is on the RAW
// BITS: -0 == +0 and NaN != NaN under ==.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fsLuaSchema = `package probe

fixed table Root
{
    a float32
    b float64
    ys [2]float64
}
`

type fsCase struct {
	name string
	b32  uint32
	b64  uint64
}

var fsCases = []fsCase{
	{"plus_zero", 0x00000000, 0x0000000000000000},
	{"minus_zero", 0x80000000, 0x8000000000000000},
	{"nan_quiet", 0x7FC00000, 0x7FF8000000000000},
	{"nan_payload", 0x7FC00001, 0x7FF8000000000001},
	{"plus_inf", 0x7F800000, 0x7FF0000000000000},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000},
}

// fsWantBits is the little-endian pattern of the four float slots: a, b,
// ys[0] and ys[1]. The Lua probe prints the same bytes through string.pack.
func fsWantBits(c fsCase) string {
	var b [4 + 8 + 8 + 8]byte
	binary.LittleEndian.PutUint32(b[0:], c.b32)
	binary.LittleEndian.PutUint64(b[4:], c.b64)
	binary.LittleEndian.PutUint64(b[12:], c.b64)
	binary.LittleEndian.PutUint64(b[20:], c.b64)
	return hex.EncodeToString(b[:])
}

func fsLuaProbe(module string, cases []fsCase) string {
	var b strings.Builder
	b.WriteString("local T = require(\"" + module + "\")\n")
	b.WriteString("local save = require(\"save\")\n")
	b.WriteString("local load = require(\"load\")\n")
	b.WriteString("local J = require(\"json\")\n")
	b.WriteString(`local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function f32(b) return string.unpack("<f", string.pack("<I4", b)) end
local function f64(b) return string.unpack("<d", string.pack("<I8", b)) end
local function bits(v)
  return hex(string.pack("<f", v.a)) .. hex(string.pack("<d", v.b))
    .. hex(string.pack("<d", v.ys[1])) .. hex(string.pack("<d", v.ys[2]))
end
local cases = {`)
	for _, c := range cases {
		fmt.Fprintf(&b, "\n  { name = %q, b32 = 0x%08x, b64 = 0x%016x },", c.name, c.b32, c.b64)
	}
	b.WriteString(`
}
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local arep = {}
assert(T.AnnounceRead(vocab, T.Announcement, arep), "announce")
for _, c in ipairs(cases) do
  local v = T.NewRoot()
  local f = f64(c.b64)
  v.a = f32(c.b32)
  v.b = f
  v.ys[1], v.ys[2] = f, f
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local wire = assert(save.Save(T, "Root", v), "variable save")
  print(c.name .. "|var|" .. hex(wire))
  local back = assert(load.LoadTolerant(T, "Root", wire, report), "variable load")
  print(c.name .. "|varback|" .. bits(back))
  local lrep = { malformed = false, refused = 0 }
  local msg = assert(T.SaveMessages("Root", vocab, { v }), "message save")
  print(c.name .. "|msg|" .. hex(msg))
  local values = assert(T.LoadMessages("Root", vocab, msg, lrep), "message load")
  print(c.name .. "|msgback|" .. bits(values[1]))
  if c.name == "plus_zero" or c.name == "minus_zero" then
    local text = assert(J.ToJson(T, "Root", v), "json write")
    print(c.name .. "|json|" .. hex(text))
    local jrep = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
    local rt = assert(J.FromJson(T, "Root", text, jrep), "json read")
    print(c.name .. "|jsonback|" .. bits(rt))
  end
end
`)
	return b.String()
}

func fsGoProbe(pkg string, cases []fsCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, `package %s

import (
	"encoding/hex"
	"math"
	"os"
	"testing"
)

func TestFSGo(t *testing.T) {
	cases := []struct {
		name string
		b32  uint32
		b64  uint64
	}{
`, pkg)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t\t{%q, 0x%08x, 0x%016x},\n", c.name, c.b32, c.b64)
	}
	b.WriteString(`	}
	for _, c := range cases {
		var v Root
		RootReset(&v)
		f := math.Float64frombits(c.b64)
		v.A = math.Float32frombits(c.b32)
		v.B = f
		v.Ys = [2]float64{f, f}
		n := RootMeasure(&v)
		wire := make([]byte, n)
		if RootSave(&v, wire) != n {
			t.Fatalf("%s: variable save", c.name)
		}
		if err := os.WriteFile("go_var_"+c.name+".hex", []byte(hex.EncodeToString(wire)), 0o644); err != nil {
			t.Fatal(err)
		}
		values := []*Root{&v}
		m := RootMeasureMessages(values, nil)
		buf := make([]byte, m)
		var rep TableReport
		if RootSaveMessages(values, buf, &rep) != int64(len(buf)) {
			t.Fatalf("%s: message save", c.name)
		}
		if err := os.WriteFile("go_msg_"+c.name+".hex", []byte(hex.EncodeToString(buf)), 0o644); err != nil {
			t.Fatal(err)
		}
		if jn := RootToJSONMeasure(&v); jn >= 0 {
			js := make([]byte, jn)
			if RootToJSON(&v, js) != jn {
				t.Fatalf("%s: json write", c.name)
			}
			if err := os.WriteFile("go_json_"+c.name+".hex", []byte(hex.EncodeToString(js)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
`)
	return b.String()
}

func fsLoadUnit(t *testing.T) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Special.schema")
	if err := os.WriteFile(schemaPath, []byte(fsLuaSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLuaPlainFloatSpecialValuesAgreeAcrossForms(t *testing.T) {
	t.Parallel()
	u := fsLoadUnit(t)

	luaFiles, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the Lua unit: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, luaFiles)
	probe := fsLuaProbe(ModuleName(u), fsCases)
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, luaDir, "main.lua", nil))
	lua := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		part := strings.SplitN(line, "|", 3)
		if len(part) != 3 {
			t.Fatalf("Lua probe line %q, want name|kind|value", line)
		}
		lua[part[0]+"|"+part[1]] = part[2]
	}

	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go unit: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go table surface: %v", err)
	}
	maps.Copy(goFiles, tables)
	goFiles["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	goFiles["floatspecial_probe_test.go"] = []byte(fsGoProbe(u.Package, fsCases))
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestFSGo$", ".")
	cmd.Dir = goDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated Go oracle: %v\n%s", err, output)
	}

	for _, c := range fsCases {
		want := fsWantBits(c)
		read := func(kind string) string {
			data, err := os.ReadFile(filepath.Join(goDir, "go_"+kind+"_"+c.name+".hex"))
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			return string(data)
		}
		if got := lua[c.name+"|var"]; got != read("var") {
			t.Errorf("%s variable: Lua saved %s, the generated Go %s", c.name, got, read("var"))
		}
		if got := lua[c.name+"|msg"]; got != read("msg") {
			t.Errorf("%s message: Lua saved %s, the generated Go %s", c.name, got, read("msg"))
		}
		if got := lua[c.name+"|varback"]; got != want {
			t.Errorf("%s variable read: Lua landed %s, want the raw bits %s", c.name, got, want)
		}
		if got := lua[c.name+"|msgback"]; got != want {
			t.Errorf("%s message read: Lua landed %s, want the raw bits %s", c.name, got, want)
		}
		// THE TEXT FORM writes -0 as -0 and reads it back as -0 (§16.3), and
		// its spelling is the generated Go writer's.
		if c.name == "plus_zero" || c.name == "minus_zero" {
			if got := lua[c.name+"|json"]; got != read("json") {
				t.Errorf("%s text: Lua wrote %s, the generated Go %s", c.name, got, read("json"))
			}
			if got := lua[c.name+"|jsonback"]; got != want {
				t.Errorf("%s text read: Lua landed %s, want the raw bits %s", c.name, got, want)
			}
		}
	}
}
