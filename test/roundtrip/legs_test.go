// The legs of the round-trip gate: how each one builds and runs its probe over
// the code its generator wrote, and the break its negative control plants. A
// leg's probe source lives in test/roundtrip/<leg>/ and calls the generated
// API only; where it is a template, the gate renders it over the unit's root
// tables, one block per root, so no root is ever dispatched by hand.
package roundtrip

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
			file: "GuardedTable.go", what: "the variable root's save corrupts its last byte",
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
	{
		name: "squirrel", lang: "squirrel", tool: "sq (the generated Squirrel modules)", retain: true,
		run: runSquirrel,
	},
	{
		name: "luau", lang: "luau", tool: "luau (the generated Luau modules)", retain: true,
		run: runLuau,
	},
	{
		name: "elixir", lang: "elixir", tool: "elixirc and elixir (the generated Elixir modules)", retain: true,
		run: runElixir,
	},
	{
		name: "lobster", lang: "lobster", tool: "lobster (the generated Lobster modules)", retain: true,
		run: runLobster,
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
func renderProbe(t testing.TB, x *rowEnv, leg, file string, extra map[string]any) []byte {
	t.Helper()
	src, err := os.ReadFile(probeSource(t, leg, file))
	if err != nil {
		t.Fatal(err)
	}
	type tmplRoot struct{ Name, Class, Snake string }
	data := map[string]any{"Package": x.pkg, "Title": title(x.pkg)}
	for k, v := range extra {
		data[k] = v
	}
	var roots []tmplRoot
	for _, r := range x.roots {
		roots = append(roots, tmplRoot{r.name, r.class, snake(r.name)})
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

func runSquirrel(t *testing.T, x *rowEnv) []byte {
	return run(t, x.gen, nil, tool("SQ", "sq"), probeSource(t, "squirrel", "probe.nut"), title(x.pkg)+"Tables", x.in)
}

// runLuau writes the cases as a module beside the probe: the luau CLI has no
// io library, so the sample texts and engine bytes travel as Luau strings and
// the saved bytes come back as RTB lines.
func runLuau(t *testing.T, x *rowEnv) []byte {
	var b strings.Builder
	b.WriteString("-- written by test/roundtrip for one row; not source\nreturn {\n\tmodules = {")
	for _, m := range genFiles(t, x.gen, ".luau") {
		if strings.HasPrefix(m, "schema_") || strings.HasPrefix(m, "serialize") || strings.HasPrefix(m, "roundtrip_") {
			continue
		}
		b.WriteString(luaString(m) + ", ")
	}
	b.WriteString("},\n\tcases = {\n")
	for _, r := range x.roots {
		for _, s := range r.samples {
			line := "\t\t{ root = " + luaString(r.name) + ", name = " + luaString(s.name) + ", class = " + luaString(r.class) + ", text = " + luaString(string(s.json))
			if s.engine != nil {
				line += ", eng = " + luaString(string(s.engine))
			}
			b.WriteString(line + " },\n")
		}
	}
	b.WriteString("\t},\n}\n")
	write(t, filepath.Join(x.gen, "roundtrip_inputs.luau"), []byte(b.String()))
	src, err := os.ReadFile(probeSource(t, "luau", "probe.luau"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(x.gen, "roundtrip_probe.luau"), src)
	return run(t, x.gen, nil, tool("LUAU", "luau"), "roundtrip_probe.luau")
}

// luaString is s as a Lua/Luau string literal: every byte outside printable
// ASCII, and the quote and backslash, as a three-digit decimal escape.
func luaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			fmt.Fprintf(&b, "\\%03d", c)
			continue
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// genFiles is the generated directory's file stems with the extension.
func genFiles(t testing.TB, dir, ext string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			out = append(out, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(out)
	return out
}

// snake is a root's name as the Elixir emitter spells its functions.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func runElixir(t *testing.T, x *rowEnv) []byte {
	var mods []string
	for _, f := range genFiles(t, x.gen, ".ex") {
		if strings.HasSuffix(f, "Fixed") && f != "FixedRuntime" {
			mods = append(mods, title(x.pkg)+"."+f)
		}
	}
	probe := renderProbe(t, x, "elixir", "probe.exs.tmpl", map[string]any{"FixedModules": "[" + strings.Join(mods, ", ") + "]"})
	write(t, filepath.Join(x.work, "probe.exs"), probe)
	ebin := filepath.Join(x.work, "ebin")
	if err := os.MkdirAll(ebin, 0o755); err != nil {
		t.Fatal(err)
	}
	var srcs []string
	for _, f := range genFiles(t, x.gen, ".ex") {
		srcs = append(srcs, filepath.Join(x.gen, f+".ex"))
	}
	run(t, x.work, nil, tool("ELIXIRC", "elixirc"), append([]string{"--ignore-module-conflict", "-o", ebin}, srcs...)...)
	return run(t, x.work, nil, tool("ELIXIR", "elixir"), "-pa", ebin, filepath.Join(x.work, "probe.exs"), x.in)
}

func runLobster(t *testing.T, x *rowEnv) []byte {
	var mods []string
	for _, f := range genFiles(t, x.gen, ".lobster") {
		if strings.HasSuffix(f, "Table") {
			mods = append(mods, f)
		}
	}
	write(t, filepath.Join(x.gen, "roundtrip_probe.lobster"), renderProbe(t, x, "lobster", "probe.lobster.tmpl", map[string]any{"Modules": mods}))
	return run(t, x.gen, nil, tool("LOBSTER", "lobster"), filepath.Join(x.gen, "roundtrip_probe.lobster"), "--", x.in)
}
