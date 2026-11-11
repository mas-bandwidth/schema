// The legs of the round-trip gate: how each one builds and runs its probe over
// the code its generator wrote, and the break its negative control plants. A
// leg's probe source lives in test/roundtrip/<leg>/ and calls the generated
// API only; where it is a template, the gate renders it over the unit's root
// tables, one block per root, so no root is ever dispatched by hand.
package roundtrip

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"unicode"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// leg is one language's row.
type leg struct {
	name   string // the row: make roundtrip-<name>
	lang   string // bin/schema generate --lang
	tool   string // what slowtest.Gate names
	retain bool   // the leg's variable root has a retaining load: e-retain is owed
	run    func(t *testing.T, x *rowEnv) []byte
	plant  plant
}

// legs is every row, in the order make roundtrip runs them.
var legs = []*leg{
	{
		name: "go", lang: "go", tool: "the Go toolchain (go test over the generated unit)", retain: true,
		run: runGo,
		plant: plant{
			file: "GuardedTable.go", what: "the variable root's save drops its last byte",
			old: "return tableRegionSave(unsafe.Pointer(value), PatrolTableType(), buffer, false, arena, allocator)",
			new: "n := tableRegionSave(unsafe.Pointer(value), PatrolTableType(), buffer, false, arena, allocator); if n > 0 { buffer[n-1] ^= 0xff }; return n",
			red: []string{"a@variable", "c-save@variable"},
		},
	},
	{
		name: "js", lang: "js", tool: "node (the generated JavaScript modules)", retain: true,
		run: runJS,
		plant: plant{
			file: "TabledemoVariable.js", what: "the variable root's load answers a default value",
			old: "export function PatrolLoad(bytes, report) {\n  return TableVariableLoad(",
			new: "export function PatrolLoad(bytes, report) {\n  return PatrolFromJson(\"{}\", report) ?? TableVariableLoad(",
			red: []string{"a@variable", "c-load@variable"},
		},
	},
	{
		name: "lua", lang: "lua", tool: "lua (the generated Lua modules)", retain: true,
		run: runLua,
		plant: plant{
			file: "save.lua", what: "every plain save writes the wrong form byte",
			old: "function S.Save(T, name, v)\n  return save_with(T, name, v, nil)",
			new: "function S.Save(T, name, v)\n  local w = save_with(T, name, v, nil)\n  if w ~= nil then w = string.char((w:byte(1) % 7) + 1) .. w:sub(2) end\n  return w",
			red: []string{"a@variable", "c-save@variable"},
		},
	},
}

// probeSource is a file of the leg's probe directory.
func probeSource(t testing.TB, leg, file string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(leg, file))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("leg %s has no probe %s: %v", leg, file, err)
	}
	return path
}

// tool is a toolchain binary: the make row passes it in the environment
// (LUA, NODE, ...), else its plain name on PATH.
func tool(env, name string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return name
}

// renderProbe renders a leg's probe template over the unit's root tables. The
// delimiters are [[ ]], so the template stays valid-looking source.
func renderProbe(t testing.TB, x *rowEnv, leg, file string, extra map[string]string) []byte {
	t.Helper()
	src, err := os.ReadFile(probeSource(t, leg, file))
	if err != nil {
		t.Fatal(err)
	}
	type tmplRoot struct{ Name, Class string }
	data := map[string]any{"Package": x.pkg, "Title": title(x.pkg)}
	for k, v := range extra {
		data[k] = v
	}
	var roots []tmplRoot
	for _, r := range x.roots {
		roots = append(roots, tmplRoot{r.name, r.class})
	}
	data["Roots"] = roots
	tmpl, err := template.New(file).Delims("[[", "]]").Option("missingkey=error").Parse(string(src))
	if err != nil {
		t.Fatalf("probe template %s/%s: %v", leg, file, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		t.Fatalf("probe template %s/%s: %v", leg, file, err)
	}
	return out.Bytes()
}

func title(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func write(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// runGo runs the probe as a test of the generated Go package, in a module that
// replaces the schema runtime with this checkout and serialize.go with the
// pinned sibling.
func runGo(t *testing.T, x *rowEnv) []byte {
	write(t, filepath.Join(x.gen, "roundtrip_probe_test.go"), renderProbe(t, x, "go", "probe_test.go.tmpl", nil))
	mod := "module roundtripprobe\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace (\n\tgithub.com/mas-bandwidth/schema/v2 => " + quote(x.repo) + "\n\tgithub.com/mas-bandwidth/serialize.go/v2 => " + quote(slowtest.SerializeGo(t, x.repo)) + "\n)\n"
	write(t, filepath.Join(x.gen, "go.mod"), []byte(mod))
	return run(t, x.gen, []string{"ROUNDTRIP_IN=" + x.in, "GOWORK=off", "GOFLAGS=-mod=mod"}, tool("GO", "go"), "test", "-count=1", "-run", "^TestRoundTripProbe$", "-v", ".")
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func runJS(t *testing.T, x *rowEnv) []byte {
	return run(t, x.repo, nil, tool("NODE", "node"), probeSource(t, "js", "probe.mjs"), x.gen, x.in)
}

func runLua(t *testing.T, x *rowEnv) []byte {
	module := title(x.pkg) + "Tables"
	if _, err := os.Stat(filepath.Join(x.gen, module+".lua")); err != nil {
		t.Fatalf("the generated Lua unit has no %s.lua", module)
	}
	return run(t, x.repo, []string{"LUA_PATH=" + filepath.Join(x.gen, "?.lua") + ";;"}, tool("LUA", "lua"), probeSource(t, "lua", "probe.lua"), module, x.in)
}
