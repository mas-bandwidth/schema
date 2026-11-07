package luatable

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// unboundedCookRow is the corpus row that carries non-empty unbounded text
// (docs/SPEC-TABLES.md §2.13.1): a `[]string` element and a `map[string]V` key.
const unboundedCookRow = "feature_full"

// unboundedCookCase generates the row's unit for Lua and answers the files, the
// row's wire and the tool's little-endian cook of it.
func unboundedCookCase(t *testing.T) (map[string][]byte, string, []byte, []byte) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	for _, r := range manifestRows(t, manifest, "instance") {
		if r.caseName != unboundedCookRow {
			continue
		}
		var paths []string
		for _, p := range units[r.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("the Lua table backend refused %s: %v", r.unit, err)
		}
		m := tabletext.NewModel(u)
		wire, err := os.ReadFile(filepath.Join(repo, r.wire))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(r.root))
		var report tabletext.Report
		if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
			t.Fatalf("decode %s: %v", r.caseName, err)
		}
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatalf("the tool's cook of %s: %v", r.caseName, err)
		}
		return files, ModuleName(u), wire, cooked
	}
	t.Fatalf("the manifest has no instance row %s", unboundedCookRow)
	return nil, "", nil, nil
}

// unboundedPatch replaces the one occurrence of each anchor in a runtime file.
func unboundedPatch(t *testing.T, files map[string][]byte, file string, edits [][2]string) {
	t.Helper()
	src := string(files[file])
	for _, e := range edits {
		if n := strings.Count(src, e[0]); n != 1 {
			t.Fatalf("the control's anchor %q appears %d times in %s, want exactly once", e[0], n, file)
		}
		src = strings.Replace(src, e[0], e[1], 1)
	}
	files[file] = []byte(src)
}

// TestLuaUnboundedTextCookNegativeControls breaks each half of the cook's
// unbounded text (docs/SPEC-TABLES.md §2.13.1) and requires the feature_full
// row to go red: the writer that writes the text inline instead of carving a
// blob no longer matches the tool's bytes, and a check or a read that does not
// follow the blob slot no longer reads the tool's cook back as the load.
func TestLuaUnboundedTextCookNegativeControls(t *testing.T) {
	t.Parallel()
	t.Run("writer_inline", func(t *testing.T) {
		t.Parallel()
		files, module, wire, want := unboundedCookCase(t)
		unboundedPatch(t, files, "cookwrite.lua", [][2]string{
			{"  if s == \"string\" and f.unbounded then\n    -- UNBOUNDED TEXT", "  if false then\n    -- UNBOUNDED TEXT"},
			{"  elseif s == \"string\" and f.unbounded then\n    return true", "  elseif false then\n    return true"},
		})
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		luarun.WriteHexProbe(t, dir)
		probe := `local T = require("` + module + `")
local load = require("load")
local cookwrite = require("cookwrite")
local inputs = require("inputs")
local hexline = require("hexline")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "Root", inputs.wire, report)
local c = v and cookwrite.Cook(T, "Root", v, nil, 1)
if c == nil then print("REFUSED") else hexline(c) end
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, _, err := luarun.Try(t, dir, "main.lua", map[string][]byte{"wire": wire})
		if err != nil {
			return // the sabotaged writer raised: red
		}
		got, herr := hex.DecodeString(strings.TrimSpace(stdout))
		if herr == nil && bytes.Equal(got, want) {
			t.Fatal("the writer that writes unbounded text inline still matched the tool's cook")
		}
	})
	for _, c := range []struct {
		name  string
		file  string
		edits [][2]string
	}{
		{"check_ignores_the_slot", "cook.lua", [][2]string{
			{"  if shape == \"string\" and f.unbounded then return text_slot(s, at) end\n", ""},
		}},
		{"read_ignores_the_slot", "uncook.lua", [][2]string{
			{"  elseif s == \"string\" and f.unbounded then\n    -- UNBOUNDED TEXT", "  elseif false then\n    -- UNBOUNDED TEXT"},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			files, module, wire, cooked := unboundedCookCase(t)
			unboundedPatch(t, files, c.file, c.edits)
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(cookReadProbe(module, "Root", false)), 0o644); err != nil {
				t.Fatal(err)
			}
			stdout, _, err := luarun.Try(t, dir, "main.lua", map[string][]byte{"wire": wire, "cook": cooked})
			if err == nil && strings.TrimSpace(stdout) == "ok" {
				t.Fatalf("the %s control stayed green", c.name)
			}
		})
	}
}

// unboundedDefault is the empty-string default beside an unbounded text
// field's marker in the generated descriptor, whatever the line breaks.
var unboundedDefault = regexp.MustCompile(`unbounded = true,\s*default = "",?`)

// TestLuaUnboundedTextResetNegativeControl drops the empty-string default an
// unbounded text field's descriptor carries (docs/SPEC-TABLES.md §2.13.1):
// D.reset then leaves the field nil where the constructor holds "", and the
// featurewitnesses unit's reset must go red.
func TestLuaUnboundedTextResetNegativeControl(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "featurewitnesses")
	inputs, roots, _ := instanceWires(t, "featurewitnesses")
	files := map[string][]byte{}
	dropped := 0
	for name, data := range b.files {
		dropped += len(unboundedDefault.FindAllIndex(data, -1))
		files[name] = unboundedDefault.ReplaceAll(data, []byte("unbounded = true,"))
	}
	if dropped == 0 {
		t.Fatal("the control's anchor, an unbounded text default, appears nowhere in the generated unit")
	}
	out, _, err := try(t, files, resetProbe(b.module, roots), inputs)
	if err == nil && !strings.Contains(out, "RESET DIFFERS") {
		t.Fatalf("reset stayed green without the unbounded default: %s", out)
	}
}
