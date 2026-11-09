package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// luaStrictCorpusSourceCheck holds internal/codegen/luatable/json_test.go and
// test/conformance/lua/main.lua to the one strict read (docs/SPEC-TABLES.md
// §6.5, §16.2): neither file calls the internal walks (fromJsonWalk, loadWalk)
// or a tolerant entry point, and json_test.go does not skip wide-field units.
func luaStrictCorpusSourceCheck(jsonTestSrc, driverSrc string) error {
	for _, banned := range []string{"fromJsonWalk", "loadWalk", "LoadTolerant", "TableWideFields"} {
		if strings.Contains(jsonTestSrc, banned) {
			return fmt.Errorf("json_test.go still carries %q", banned)
		}
	}
	for _, want := range []string{"J.FromJson(", "load.Load(", "T.ReadFixed("} {
		if !strings.Contains(jsonTestSrc, want) {
			return fmt.Errorf("json_test.go is missing strict read call %q", want)
		}
	}
	for _, banned := range []string{"fromJsonWalk", "loadWalk", "LoadTolerant"} {
		if strings.Contains(driverSrc, banned) {
			return fmt.Errorf("test/conformance/lua/main.lua still carries %q", banned)
		}
	}
	for _, want := range []string{"u.json.FromJson(", "u.load.Load("} {
		if !strings.Contains(driverSrc, want) {
			return fmt.Errorf("test/conformance/lua/main.lua is missing strict read call %q", want)
		}
	}
	return nil
}

type strictCorpusItem struct {
	kind     string // "instance" or "hostile"
	caseName string
	root     string
	fixed    bool
	expected string
	textKey  string
	wireKey  string
	textHex  string
	wireHex  string
}

func strictCorpusUnitProbe(module string, items []strictCorpusItem) string {
	var b strings.Builder
	b.WriteString(`local T = require("` + module + `")
local J = require("json")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local items = {
`)
	for i, it := range items {
		fmt.Fprintf(&b, "  { idx = %d, kind = %q, name = %q, root = %q, fixed = %t, expected = %q, textKey = %q, wireKey = %q },\n",
			i, it.kind, it.caseName, it.root, it.fixed, it.expected, it.textKey, it.wireKey)
	}
	b.WriteString(`}
for _, it in ipairs(items) do
  if it.kind == "instance" then
    local r1 = {}
    local v1 = J.FromJson(T, it.root, inputs[it.textKey], r1)
    local ok1 = v1 ~= nil and not r1.damaged and (r1.refused or 0) == 0 and r1.reason == nil
    local saved = nil
    if ok1 then
      if it.fixed then
        saved = T.FixedSave(it.root, { v1 })
      else
        saved = save.Save(T, it.root, v1)
      end
    end
    local v2, ok2
    if it.fixed then
      v2 = T.ReadFixed(it.root, inputs[it.wireKey])
      ok2 = v2 ~= nil
    else
      local r2 = {}
      v2 = load.Load(T, it.root, inputs[it.wireKey], r2)
      ok2 = v2 ~= nil and not r2.damaged and (r2.refused or 0) == 0 and r2.reason == nil
    end
    local text = ok2 and J.ToJson(T, it.root, v2) or nil
    print(string.format("%d|%s|%s|%s|%s", it.idx, ok1 and "0" or "1", ok2 and "0" or "1", hex(saved), hex(text)))
  else
    local r = {}
    local v = J.FromJson(T, it.root, inputs[it.textKey], r)
    local verdict
    if v == nil then
      if (r.refused or 0) == 1 and type(r.reason) == "string" and r.reason ~= "" then
        verdict = "refused"
      else
        verdict = "refused-unmarked"
      end
    elseif not r.damaged and (r.refused or 0) == 0 and r.reason == nil then
      verdict = "read"
    else
      verdict = "read-damaged"
    end
    print(string.format("%d|%s", it.idx, verdict))
  end
end
`)
	return b.String()
}

// runLuaStrictCorpus runs every text-carrying instance row and every
// json-hostile row of MANIFEST.txt, plus the variable-class null-reference and
// skipped-object duplicate-key checks, through the strict J.FromJson and
// load.Load / T.ReadFixed with an optional patch applied to json.lua.
func runLuaStrictCorpus(t *testing.T, patch func([]byte) ([]byte, error)) error {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}

	instanceRows := jsonManifestRows(t, manifestPath, "instance")
	hostileRows := jsonManifestRows(t, manifestPath, "json-hostile")

	var unitOrder []string
	seenUnit := map[string]bool{}
	for _, r := range instanceRows {
		if !r.noText && !seenUnit[r.unit] {
			seenUnit[r.unit] = true
			unitOrder = append(unitOrder, r.unit)
		}
	}
	for _, r := range hostileRows {
		if !seenUnit[r.unit] {
			seenUnit[r.unit] = true
			unitOrder = append(unitOrder, r.unit)
		}
	}
	sort.Strings(unitOrder)

	readCompared, writeCompared, hostileCompared := 0, 0, 0
	for _, unitName := range unitOrder {
		paths, ok := unitFiles[unitName]
		if !ok {
			return fmt.Errorf("unit %s: not in manifest", unitName)
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			continue
		}
		if patch != nil {
			patched, perr := patch(files["json.lua"])
			if perr != nil {
				return perr
			}
			files["json.lua"] = patched
		}

		var items []strictCorpusItem
		inputs := map[string][]byte{}
		for _, row := range instanceRows {
			if row.noText || row.unit != unitName {
				continue
			}
			textData, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", row.caseName+".json"))
			if err != nil {
				return fmt.Errorf("instance %s: read text: %v", row.caseName, err)
			}
			wireData, err := os.ReadFile(filepath.Join(repo, row.path))
			if err != nil {
				return fmt.Errorf("instance %s: read wire: %v", row.caseName, err)
			}
			tk := fmt.Sprintf("it_%d", len(items))
			wk := fmt.Sprintf("iw_%d", len(items))
			inputs[tk] = textData
			inputs[wk] = wireData
			st := u.Tables[row.root]
			items = append(items, strictCorpusItem{
				kind:     "instance",
				caseName: row.caseName,
				root:     row.root,
				fixed:    st != nil && st.FixedDeclared,
				textKey:  tk,
				wireKey:  wk,
				textHex:  hexBytes(textData),
				wireHex:  hexBytes(wireData),
			})
		}
		for _, row := range hostileRows {
			if row.unit != unitName {
				continue
			}
			textData, err := os.ReadFile(filepath.Join(repo, row.path, row.root+".json"))
			if err != nil {
				return fmt.Errorf("json-hostile %s: read text: %v", row.caseName, err)
			}
			tk := fmt.Sprintf("ht_%d", len(items))
			inputs[tk] = textData
			items = append(items, strictCorpusItem{
				kind:     "hostile",
				caseName: row.caseName,
				root:     row.root,
				expected: row.expected,
				textKey:  tk,
			})
		}
		if len(items) == 0 {
			continue
		}

		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := strictCorpusUnitProbe(ModuleName(u), items)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			return err
		}
		out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n")
		lines := strings.Split(out, "\n")
		if len(lines) != len(items) {
			return fmt.Errorf("unit %s: got %d output lines, want %d", unitName, len(lines), len(items))
		}
		for i, line := range lines {
			it := items[i]
			parts := strings.Split(line, "|")
			if it.kind == "instance" {
				if len(parts) != 5 {
					return fmt.Errorf("instance %s: bad output line %q", it.caseName, line)
				}
				if parts[1] != "0" || parts[2] != "0" {
					return fmt.Errorf("instance %s: strict read refused (fromJson=%s, load=%s)", it.caseName, parts[1], parts[2])
				}
				if parts[3] != it.wireHex {
					return fmt.Errorf("instance %s: FromJson then Save != wire", it.caseName)
				}
				readCompared++
				if parts[4] != it.textHex {
					return fmt.Errorf("instance %s: Load then ToJson != text", it.caseName)
				}
				writeCompared++
			} else {
				if len(parts) != 2 {
					return fmt.Errorf("json-hostile %s: bad output line %q", it.caseName, line)
				}
				if parts[1] != it.expected {
					return fmt.Errorf("json-hostile %s: got %q, want %q", it.caseName, parts[1], it.expected)
				}
				hostileCompared++
			}
		}
	}
	if readCompared < 113 || writeCompared < 113 || hostileCompared < 155 {
		return fmt.Errorf("corpus coverage short: read=%d write=%d hostile=%d", readCompared, writeCompared, hostileCompared)
	}

	// Variable-class null references (§16.2) and duplicate keys inside a
	// skipped unknown object: read through the strict J.FromJson.
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "N.schema")
	if err := os.WriteFile(schemaPath, []byte(`package n
union Payload
{
    data *bytes
    tally int32
}
table N
{
    blobs map[int32]*bytes
    body  Payload
}
`), 0o644); err != nil {
		return err
	}
	u := luarun.LoadUnit(t, schemaPath)
	files, err := Generate(u)
	if err != nil {
		return err
	}
	if patch != nil {
		patched, perr := patch(files["json.lua"])
		if perr != nil {
			return perr
		}
		files["json.lua"] = patched
	}
	runDir := t.TempDir()
	luarun.WriteFiles(t, runDir, files)
	module := ModuleName(u)
	nullProbe := `local T = require("` + module + `")
local J = require("json")
local inputs = require("inputs")
local r = {}
local v = J.FromJson(T, "N", inputs.nulls, r)
local has = v ~= nil and not r.damaged and (r.refused or 0) == 0 and r.kindChanged == 0
  and v.blobs[7] == false and v.blobs[8] ~= nil and v.blobs[8].value == "\1\2"
  and v.body.tag == 1 and v.body.data == nil
local text = v and J.ToJson(T, "N", v) or ""
local rdup1 = {}
local vdup1 = J.FromJson(T, "N", inputs.dup1, rdup1)
local rdup2 = {}
local vdup2 = J.FromJson(T, "N", inputs.dup2, rdup2)
local dupRefused = vdup1 == nil and (rdup1.refused or 0) == 1 and vdup2 == nil and (rdup2.refused or 0) == 1
print(string.format("nulls=%s dupRefused=%s", tostring(has), tostring(dupRefused)))
print(text)
`
	if err := os.WriteFile(filepath.Join(runDir, "main.lua"), []byte(nullProbe), 0o644); err != nil {
		return err
	}
	out := strings.TrimRight(string(luarun.Run(t, runDir, "main.lua", map[string][]byte{
		"nulls": []byte("{ \"blobs\": { \"7\": null, \"8\": \"AQI=\" }, \"body\": { \"data\": null } }\n"),
		"dup1":  []byte("{ \"blobs\": {}, \"future\": { \"x\": 1, \"x\": 2 } }\n"),
		"dup2":  []byte("{ \"blobs\": {}, \"future\": { \"x\": 1, \"\\u0078\": 2 } }\n"),
	})), "\n")
	lines := strings.SplitN(out, "\n", 2)
	if lines[0] != "nulls=true dupRefused=true" {
		return fmt.Errorf("variable-class null/duplicate check failed: %q", lines[0])
	}
	if len(lines) < 2 || !strings.Contains(lines[1], "\"7\": null") || !strings.Contains(lines[1], "\"data\": null") {
		return fmt.Errorf("null references did not write back as null: %q", out)
	}
	return nil
}

// TestLuaFromJsonStrictAgreesWithTheCorpus closes the STRICT-100 gap for
// port/lua/vt/text-form and port/lua/vt/text-form-variable: the corpus and
// null-reference tests and the Lua conformance driver read strictly through
// J.FromJson and load.Load / T.ReadFixed (never fromJsonWalk or loadWalk), and
// every text-carrying instance row (113 rows, wide-field units included),
// every json-hostile row (155 rows), variable-class null references, and
// unknown-subtree duplicate keys obey the strict read.
func TestLuaFromJsonStrictAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")
	jsonTestBytes, err := os.ReadFile("json_test.go")
	if err != nil {
		t.Fatal(err)
	}
	driverBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "lua", "main.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if err := luaStrictCorpusSourceCheck(string(jsonTestBytes), string(driverBytes)); err != nil {
		t.Fatal(err)
	}
	if err := runLuaStrictCorpus(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestLuaFromJsonStrictAgreesWithTheCorpusNegativeControl plants faults in the
// real artifacts — the walk names in json_test.go and the strict-refusal,
// unknown-subtree duplicate-key, and field-lookup clauses in runtime/json.lua —
// and requires each plant to turn the gate red.
func TestLuaFromJsonStrictAgreesWithTheCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")

	jsonTestBytes, err := os.ReadFile("json_test.go")
	if err != nil {
		t.Fatal(err)
	}
	driverBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "lua", "main.lua"))
	if err != nil {
		t.Fatal(err)
	}
	jsonTestSrc := string(jsonTestBytes)
	driverSrc := string(driverBytes)

	for _, s := range []struct {
		name, anchor, replacement string
	}{
		{"fromJsonWalk in json_test.go", "J.FromJson(", "J.fromJsonWalk("},
		{"loadWalk in json_test.go", "load.Load(", "load.loadWalk("},
	} {
		if !strings.Contains(jsonTestSrc, s.anchor) {
			t.Fatalf("%s: anchor %q missing from json_test.go", s.name, s.anchor)
		}
		planted := strings.Replace(jsonTestSrc, s.anchor, s.replacement, 1)
		if err := luaStrictCorpusSourceCheck(planted, driverSrc); err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s left luaStrictCorpusSourceCheck green", s.name)
		}
	}

	for _, p := range []struct {
		name, anchor, replacement string
	}{
		{
			"strict refusal bypass in FromJson",
			"  if reason ~= nil then\n    report.refused = 1\n",
			"  if false then\n    report.refused = 1\n",
		},
		{
			"unknown-object duplicate key in skip_container",
			"      if seen[key] then\n        inp.bad = true\n        return false\n      end\n",
			"      if false then\n        inp.bad = true\n        return false\n      end\n",
		},
		{
			"field lookup in find_key",
			"  for _, fd in ipairs(desc.fields) do\n    if fd.key == key then return fd end\n  end\n",
			"  for _, fd in ipairs(desc.fields) do\n    if false then return fd end\n  end\n",
		},
	} {
		err := runLuaStrictCorpus(t, func(src []byte) ([]byte, error) {
			s := string(src)
			if n := strings.Count(s, p.anchor); n != 1 {
				return nil, fmt.Errorf("%s: anchor matched %d times, want 1", p.name, n)
			}
			return []byte(strings.Replace(s, p.anchor, p.replacement, 1)), nil
		})
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s left runLuaStrictCorpus green", p.name)
		}
		t.Logf("%s: caught (%v)", p.name, err)
	}
}
