// Package luarun runs generated Lua under the pinned interpreter for the Lua
// leg's Go tests (docs/PORT-LUA.md, THE HARNESS). It is a TEST HELPER: it
// writes a probe's inputs into the test's own directory, runs one lua process
// there and hands back its stdout. No network, no clock, no service: the
// interpreter reads files in dir and writes to stdout.
//
// The interpreter is $LUA when set (make/lua.mk exports the pin), else the
// repository's dist/lua-5.4.8/src/lua (make dist-lua). An interpreter that
// does not resolve FAILS the test and names make dist-lua; it never skips,
// because a leg that skips is a gate with no blade (issue #599).
package luarun

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// PinnedVersion is the interpreter make/lua.mk pins (docs/PORT-LUA.md).
const PinnedVersion = "5.4.8"

// Interpreter returns the pinned lua binary, or fails t naming make dist-lua.
func Interpreter(t testing.TB) string {
	t.Helper()
	if p := os.Getenv("LUA"); p != "" {
		if _, err := exec.LookPath(p); err != nil {
			t.Fatalf("LUA=%s does not resolve (%v): run make dist-lua, or point LUA at a Lua %s interpreter", p, err, PinnedVersion)
		}
		return p
	}
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the Lua interpreter does not resolve: %v; run make dist-lua (docs/PORT-LUA.md)", err)
	}
	p := filepath.Join(root, "dist", "lua-"+PinnedVersion, "src", "lua")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the Lua interpreter does not resolve: %s is absent and LUA is unset; run make dist-lua (docs/PORT-LUA.md)", p)
	}
	return p
}

// RepoRoot walks up from the working directory to the directory holding the
// module's go.mod.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above the working directory")
		}
		dir = parent
	}
}

// Run writes inputs as the module inputs.lua in dir (each key a Lua string
// holding exactly those bytes), runs dir/main under the pinned interpreter
// with dir as the working directory and the head of package.path, and
// returns stdout. A nonzero exit fails t with stderr.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	stdout, stderr, err := Try(t, dir, main, inputs)
	if err != nil {
		t.Fatalf("lua %s: %v\nstderr:\n%s\nstdout:\n%s", main, err, stderr, stdout)
	}
	return []byte(stdout)
}

// Try is Run for a probe that is EXPECTED to fail: the negative controls
// sabotage a runtime and require the probe to go red, so they need the
// failure handed back with both streams and not raised. An interpreter that
// does not resolve still fails t.
func Try(t testing.TB, dir, main string, inputs map[string][]byte) (stdout, stderr string, err error) {
	t.Helper()
	lua := Interpreter(t)
	if werr := os.WriteFile(filepath.Join(dir, "inputs.lua"), InputsModule(inputs), 0o644); werr != nil {
		t.Fatal(werr)
	}
	cmd := exec.Command(lua, main)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LUA_PATH="+filepath.Join(dir, "?.lua")+";;", "LUA_INIT=")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// InputsModule renders inputs as a Lua module returning a table of byte
// strings, keys sorted so the file is deterministic. Every byte is a \xNN
// escape, so any content survives the Lua lexer.
func InputsModule(inputs map[string][]byte) []byte {
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("-- written by luarun.Run: the probe's inputs as byte strings\nreturn {\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  [%s] = %s,\n", Quote([]byte(k)), Quote(inputs[k]))
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// Quote renders bytes as a Lua string literal of \xNN escapes.
func Quote(data []byte) string {
	var b strings.Builder
	b.Grow(2 + 4*len(data))
	b.WriteByte('"')
	for _, c := range data {
		fmt.Fprintf(&b, "\\x%02x", c)
	}
	b.WriteByte('"')
	return b.String()
}

// HexProbe is the Lua side of the probe convention: hexline(s) prints the
// bytes of s as ONE line of lowercase hex. A probe requires it as
// `local hexline = require("hexline")` after WriteHexProbe put it in dir.
const HexProbe = `-- written by luarun: print a byte string as one lowercase hex line
return function(s)
  io.write((s:gsub(".", function(c) return string.format("%02x", c:byte()) end)), "\n")
end
`

// WriteHexProbe writes the hexline module into dir.
func WriteHexProbe(t testing.TB, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "hexline.lua"), []byte(HexProbe), 0o644); err != nil {
		t.Fatal(err)
	}
}

// HexLines decodes a probe's stdout: one lowercase hex line per byte string.
// A line that is not lowercase hex fails t.
func HexLines(t testing.TB, out []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" && len(out) == 0 {
			break
		}
		if strings.ToLower(line) != line {
			t.Fatalf("probe line %q is not lowercase hex", line)
		}
		b, err := hex.DecodeString(line)
		if err != nil {
			t.Fatalf("probe line %q is not hex: %v", line, err)
		}
		lines = append(lines, b)
	}
	return lines
}

// LoadUnit builds one compilation unit from schema paths, the manifest's
// rule: a directory contributes its *.schema files, a named file itself.
func LoadUnit(t testing.TB, paths ...string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			ast, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path:  name,
				Name:  filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data,
				AST:   ast,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// WriteFiles writes generated files into dir, in byte order of their names. On
// a case-insensitive filesystem a unit's Save.lua and the runtime's save.lua
// are one file, and map order would pick the winner at random; byte order
// writes the capitalized module first and the runtime after it, so the
// runtime a table module requires is the one that is there, on every host.
func WriteFiles(t testing.TB, dir string, files map[string][]byte) {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// DumpProbe is the Lua side of the canonical value dump the leg's tests
// compare against an expectation computed in Go from the IR: an integer is
// i:<decimal of its 64 bits>, a float f:<the 16 hex digits of its double's
// bits>, a boolean b:<true|false>, a string s:<lowercase hex of its bytes>,
// and a table {k=v,...} with integer keys first, ascending, written [n], then
// string keys in byte order. Functions are skipped, so a module table dumps
// as its data.
const DumpProbe = `-- written by luarun: the canonical value dump (luarun.DumpProbe)
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function dump(v)
  local t = type(v)
  if t == "number" then
    if math.type(v) == "integer" then return "i:" .. string.format("%d", v) end
    return "f:" .. string.format("%016x", (string.unpack("<i8", string.pack("<d", v))))
  elseif t == "boolean" then
    return "b:" .. tostring(v)
  elseif t == "string" then
    return "s:" .. hex(v)
  elseif t == "table" then
    local nums, strs = {}, {}
    for k, x in pairs(v) do
      if type(x) ~= "function" then
        if type(k) == "number" then nums[#nums + 1] = k else strs[#strs + 1] = k end
      end
    end
    table.sort(nums)
    table.sort(strs)
    local parts = {}
    for _, k in ipairs(nums) do parts[#parts + 1] = "[" .. string.format("%d", k) .. "]=" .. dump(v[k]) end
    for _, k in ipairs(strs) do parts[#parts + 1] = k .. "=" .. dump(v[k]) end
    return "{" .. table.concat(parts, ",") .. "}"
  end
  return "?" .. t
end
return dump
`

// WriteDumpProbe writes the dump module into dir, for require("dump").
func WriteDumpProbe(t testing.TB, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "dump.lua"), []byte(DumpProbe), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Dump values, the Go side of DumpProbe: build an expectation with these and
// compare it with what the probe printed.

// DumpInt is an integer's dump: its 64 bits as a signed decimal.
func DumpInt(bits uint64) string { return "i:" + strconv.FormatInt(int64(bits), 10) }

// DumpFloat is a float's dump: the bits of the double it is.
func DumpFloat(v float64) string { return fmt.Sprintf("f:%016x", math.Float64bits(v)) }

// DumpBool is a boolean's dump.
func DumpBool(v bool) string { return "b:" + strconv.FormatBool(v) }

// DumpString is a byte string's dump.
func DumpString(b []byte) string { return "s:" + hex.EncodeToString(b) }

// DumpTable is a table's dump from its already-dumped entries: seq is the
// 1-based sequence part, named the string keys.
func DumpTable(seq []string, named map[string]string) string {
	parts := make([]string, 0, len(seq)+len(named))
	for i, v := range seq {
		parts = append(parts, fmt.Sprintf("[%d]=%s", i+1, v))
	}
	keys := make([]string, 0, len(named))
	for k := range named {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+named[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}
