package luatable

// THE FLOAT TEXT IS THE C LOCALE'S SPELLING (docs/SPEC-TABLES.md §16.14 W12:
// `printf("%.*g", p, v)` in the C locale; "Every implementation writes the
// SAME BYTES"). The host process may set LC_NUMERIC to a locale whose decimal
// point is a comma; the Lua JSON writer still writes 1.5, never 1,5, and the
// reader reads its own output back. The bytes are held to the compiler's own
// engine (internal/tabletext), which reads the leg's wire and writes its text.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const luaJSONFloatLocaleSchema = `package localefloat
table Root {
 precision float64
 ratings [..4]float32
}
`

// luaJSONFloatLocaleTried are the comma-decimal numeric locales the probe
// tries, in order; the first one os.setlocale accepts is used.
var luaJSONFloatLocaleTried = []string{"de_DE.UTF-8", "de_DE.utf8", "de_DE", "fr_FR.UTF-8", "fr_FR.utf8", "fr_FR", "nl_NL.UTF-8", "nl_NL.utf8"}

// luaJSONFloatLocaleProbe sets a comma-decimal numeric locale, then writes
// Root.wire and Root.json for precision 1.5 and ratings {0.25, 2.5}, reads the
// text back with the plain FromJson and prints one verdict line.
const luaJSONFloatLocaleProbe = `local T = require("LocalefloatTables")
local save = require("save")
local J = require("json")

local dir = arg[1]
local tried = {}
local chosen = nil
for i = 2, #arg do
  tried[#tried + 1] = arg[i]
  if os.setlocale(arg[i], "numeric") ~= nil and string.format("%.1f", 0.5):find(",", 1, true) then
    chosen = arg[i]
    break
  end
end
if chosen == nil then
  print("nolocale|" .. table.concat(tried, " "))
  return
end

local v = T.NewRoot()
v.precision = 1.5
v.ratings = { 0.25, 2.5 }
local wire = assert(save.Save(T, "Root", v))
local text = assert(J.ToJson(T, "Root", v))
local w = assert(io.open(dir .. "/Root.wire", "wb"))
w:write(wire)
w:close()
local j = assert(io.open(dir .. "/Root.json", "wb"))
j:write(text)
j:close()

local back = J.FromJson(T, "Root", text, {})
if back == nil then
  print("readback|refused|" .. chosen)
elseif back.precision == 1.5 and #back.ratings == 2 and back.ratings[1] == 0.25 and back.ratings[2] == 2.5 then
  print("readback|equal|" .. chosen)
else
  print("readback|differs|" .. chosen)
end
`

// luaJSONFloatLocalePath compiles de_DE.UTF-8 into dir with localedef where
// the host has the tool and the locale sources, and returns dir for LOCPATH;
// it returns "" where it cannot, and the probe then tries the installed
// locales.
func luaJSONFloatLocalePath(t *testing.T) string {
	t.Helper()
	localedef, err := exec.LookPath("localedef")
	if err != nil {
		return ""
	}
	dir := t.TempDir()
	cmd := exec.Command(localedef, "-i", "de_DE", "-f", "UTF-8", filepath.Join(dir, "de_DE.UTF-8"))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("localedef could not build de_DE.UTF-8 (%v); trying the installed locales\n%s", err, out)
		return ""
	}
	return dir
}

// TestLuaJsonFloatTextIgnoresLocale writes a float64 and a float32 array under
// a comma-decimal LC_NUMERIC: the text is the engine's, byte for byte, and the
// plain read takes it back to equal values. A host with no comma-decimal
// locale FAILS by name; it never skips.
func TestLuaJsonFloatTextIgnoresLocale(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")
	lua := luarun.Interpreter(t)
	dir := t.TempDir()
	schema := filepath.Join(dir, "localefloat.schema")
	if err := os.WriteFile(schema, []byte(luaJSONFloatLocaleSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	mod := t.TempDir()
	luarun.WriteFiles(t, mod, files)
	if err := os.WriteFile(filepath.Join(mod, "main.lua"), []byte(luaJSONFloatLocaleProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	cmd := exec.Command(lua, append([]string{"main.lua", out}, luaJSONFloatLocaleTried...)...)
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "LUA_PATH="+filepath.Join(mod, "?.lua")+";;", "LUA_INIT=")
	if locpath := luaJSONFloatLocalePath(t); locpath != "" {
		cmd.Env = append(cmd.Env, "LOCPATH="+locpath)
	}
	res, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, res)
	}
	line := strings.TrimRight(string(res), "\n")
	if tried, ok := strings.CutPrefix(line, "nolocale|"); ok {
		t.Fatalf("no comma-decimal numeric locale is available to os.setlocale; tried %s (install one, or the locale sources for localedef)", tried)
	}
	if !strings.HasPrefix(line, "readback|") {
		t.Fatalf("bad probe output %q", line)
	}

	legJSON, err := os.ReadFile(filepath.Join(out, "Root.json"))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(out, "Root.wire"))
	if err != nil {
		t.Fatal(err)
	}
	engineDir := t.TempDir()
	report, err := tablepack.UnpackOneFile(tabletext.NewModel(u), "Root", wire, engineDir)
	if err != nil {
		t.Fatalf("the engine could not read the leg's wire: %v", err)
	}
	if report != (tabletext.Report{}) {
		t.Fatalf("the engine's read of the leg's wire is not clean: %+v", report)
	}
	engineJSON, err := os.ReadFile(filepath.Join(engineDir, "Root.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legJSON, engineJSON) {
		t.Errorf("under %s the leg's text is not the engine's:\nleg:    %q\nengine: %q", strings.Split(line, "|")[2], legJSON, engineJSON)
	}
	if !strings.HasPrefix(line, "readback|equal|") {
		t.Errorf("the plain read of the leg's own text: %s\ntext: %q", line, legJSON)
	}
}
