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
	"slices"
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
	lang   string // generate --lang
	bin    string // the compiler, repository-relative; empty is bin/schema
	tool   string // what slowtest.Gate names
	retain bool   // the leg's variable root has a retaining load: e-retain is owed
	run    func(t *testing.T, x *rowEnv) []byte
	plant  plant
}

// compiler is the binary the leg generates with.
func (l *leg) compiler() string {
	if l.bin == "" {
		return "bin/schema"
	}
	return l.bin
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
			red: []string{"examples:a@variable", "examples:c-save@variable"},
		},
	},
	{
		name: "js", lang: "js", tool: "node (the generated JavaScript modules)", retain: true,
		run: runJS,
		plant: plant{
			file: "TabledemoVariable.js", what: "the variable root's load answers a default value",
			old: "export function PatrolLoad(bytes, report) {\n  return TableVariableLoad(",
			new: "export function PatrolLoad(bytes, report) {\n  return PatrolFromJson(\"{}\", report) ?? TableVariableLoad(",
			red: []string{"examples:a@variable", "examples:c-load@variable"},
		},
	},
	{
		name: "lua", lang: "lua", tool: "lua (the generated Lua modules)", retain: true,
		run: runLua,
		plant: plant{
			file: "save.lua", what: "every plain save writes the wrong form byte",
			old: "function S.Save(T, name, v)\n  return save_with(T, name, v, nil)",
			new: "function S.Save(T, name, v)\n  local w = save_with(T, name, v, nil)\n  if w ~= nil then w = string.char((w:byte(1) % 7) + 1) .. w:sub(2) end\n  return w",
			red: []string{"examples:a@variable", "examples:c-save@variable"},
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
		name: "moo", lang: "moo", bin: "build/schema-moo", tool: "moo (the generated moo modules, under the pinned interpreter)", retain: true,
		run: runMoo,
	},
	{
		name: "elixir", lang: "elixir", tool: "elixirc and elixir (the generated Elixir modules)", retain: true,
		run: runElixir,
	},
	{
		name: "lobster", lang: "lobster", tool: "lobster (the generated Lobster modules)", retain: true,
		run: runLobster,
	},
	{
		name: "dart", lang: "dart", tool: "dart (the generated Dart libraries)", retain: true,
		run: runDart,
	},
	{
		name: "java", lang: "java", tool: "javac and java (the generated Java package)", retain: true,
		run: runJava,
	},
	{
		name: "cs", lang: "cs", tool: "dotnet (the generated C# sources)", retain: true,
		run: runCS,
	},
	{
		name: "rust", lang: "rust", tool: "cargo (the generated Rust crate)", retain: true,
		run: runRust,
	},
	{
		name: "c", lang: "c", tool: "cc (the generated C sources)", retain: true,
		run: runC,
	},
	{
		name: "cpp", lang: "cpp", tool: "c++ (the generated C++ sources)", retain: true,
		run: runCPP,
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
	type tmplRoot struct{ Name, Class, Snake, Lower string }
	data := map[string]any{"Package": x.pkg, "Title": title(x.pkg)}
	for k, v := range extra {
		data[k] = v
	}
	var roots []tmplRoot
	for _, r := range x.roots {
		roots = append(roots, tmplRoot{r.name, r.class, snake(r.name), lower(r.name)})
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

// lower is s with its first letter lower case: the per-root function prefix
// of the Dart, Java and C# legs.
func lower(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// homes is, per root, the generated file (stem) that defines it: the first
// file with the extension whose text holds decl(root). A root no file defines
// is absent, and the probe answers that it has no generated path.
func homes(t testing.TB, x *rowEnv, ext string, decl func(root string) string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range genFiles(t, x.gen, ext) {
		src, err := os.ReadFile(filepath.Join(x.gen, f+ext))
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range x.roots {
			if _, done := out[r.name]; !done && bytes.Contains(src, []byte(decl(r.name))) {
				out[r.name] = f
			}
		}
	}
	return out
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
	sourceInputs(t, x, ".luau", "luau", "probe.luau")
	return run(t, x.gen, nil, tool("LUAU", "luau"), "roundtrip_probe.luau")
}

// runMoo is luau's runner under the pinned moo interpreter: no io library,
// inputs as source and saved bytes printed.
func runMoo(t *testing.T, x *rowEnv) []byte {
	sourceInputs(t, x, ".moo", "moo", "probe.moo")
	return run(t, x.gen, nil, tool("MOO", "moo"), "roundtrip_probe.moo")
}

// sourceInputs writes the unit's module list and every case (sample text and
// engine bytes) as the module roundtrip_inputs<ext>, and copies the leg's probe
// beside it as roundtrip_probe<ext>, for a runtime with no io library.
func sourceInputs(t *testing.T, x *rowEnv, ext, legDir, probe string) {
	var b strings.Builder
	b.WriteString("-- written by test/roundtrip for one row; not source\nreturn {\n\tmodules = {")
	for _, m := range genFiles(t, x.gen, ext) {
		if !strings.HasSuffix(m, "Table") {
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
	write(t, filepath.Join(x.gen, "roundtrip_inputs"+ext), []byte(b.String()))
	src, err := os.ReadFile(probeSource(t, legDir, probe))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(x.gen, "roundtrip_probe"+ext), src)
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

// runDart renders the probe into the generated directory, importing each
// <File>Fixed.dart that defines a fixed root's <root>FixedLoad.
func runDart(t *testing.T, x *rowEnv) []byte {
	home := homes(t, x, ".dart", func(r string) string { return "int " + lower(r) + "FixedLoad(" })
	var imports []string
	for _, f := range home {
		if !slices.Contains(imports, f) {
			imports = append(imports, f)
		}
	}
	sort.Strings(imports)
	write(t, filepath.Join(x.gen, "roundtrip_probe.dart"), renderProbe(t, x, "dart", "probe.dart.tmpl", map[string]any{"FixedHome": home, "Imports": imports}))
	return run(t, x.gen, nil, tool("DART", "dart"), "run", "roundtrip_probe.dart", x.in)
}

// runJava compiles the rendered probe with the generated package and runs it.
func runJava(t *testing.T, x *rowEnv) []byte {
	src := filepath.Join(x.work, "src")
	write(t, filepath.Join(src, "Probe.java"), renderProbe(t, x, "java", "Probe.java.tmpl", nil))
	files := []string{filepath.Join(src, "Probe.java")}
	for _, f := range genFiles(t, x.gen, ".java") {
		files = append(files, filepath.Join(x.gen, f+".java"))
	}
	classes := filepath.Join(x.work, "classes")
	run(t, x.work, nil, tool("JAVAC", "javac"), append([]string{"-nowarn", "-encoding", "UTF-8", "-d", classes}, files...)...)
	return run(t, x.work, nil, tool("JAVA", "java"), "-cp", classes, "Probe", x.in)
}

// sibling is a pinned sibling runtime checkout: the environment variable the
// make row passes, else <name> beside the repository (the fleet stages both).
func sibling(t testing.TB, x *rowEnv, env, name string) string {
	t.Helper()
	dir := os.Getenv(env)
	if dir == "" {
		dir = filepath.Join(filepath.Dir(x.repo), name)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the pinned sibling %s is missing (set %s): %v", name, env, err)
	}
	return dir
}

// runCS builds the rendered probe with the generated sources and the pinned
// serialize.cs runtime, then runs it.
func runCS(t *testing.T, x *rowEnv) []byte {
	proj := filepath.Join(x.work, "cs")
	write(t, filepath.Join(proj, "Probe.cs"), renderProbe(t, x, "cs", "Probe.cs.tmpl", nil))
	ser := filepath.Join(sibling(t, x, "SERIALIZE_CS", "serialize.cs"), "src")
	csproj := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>disable</Nullable>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AssemblyName>roundtripprobe</AssemblyName>
    <NoWarn>$(NoWarn);CS0618</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Probe.cs" />
    <Compile Include="` + x.gen + `/*.cs" />
    <Compile Include="` + ser + `/Serialize.cs" />
    <Compile Include="` + ser + `/Int128Pair.cs" />
  </ItemGroup>
</Project>
`
	write(t, filepath.Join(proj, "roundtripprobe.csproj"), []byte(csproj))
	dotnet := tool("DOTNET", "dotnet")
	run(t, proj, []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1"}, dotnet, "build", "-c", "Release", "--nologo", "-v", "quiet", "-o", filepath.Join(proj, "out"))
	return run(t, proj, []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1"}, dotnet, filepath.Join(proj, "out", "roundtripprobe.dll"), x.in)
}

// runRust builds the rendered probe as the binary of a package whose library
// is the generated crate (both of its accelerator features on, its default),
// against the pinned serialize.rs, and runs it. The target directory lives
// under build/ so the runtime dependency compiles once per bench.
func runRust(t *testing.T, x *rowEnv) []byte {
	pkg := filepath.Join(x.work, "rs")
	write(t, filepath.Join(pkg, "src", "main.rs"), renderProbe(t, x, "rust", "main.rs.tmpl", nil))
	manifest := `[package]
name = "roundtripprobe"
version = "0.0.0"
edition = "2024"

[lib]
name = "` + x.pkg + `"
path = "` + filepath.Join(x.gen, "lib.rs") + `"

[[bin]]
name = "roundtripprobe"
path = "src/main.rs"

[features]
default = ["block", "cook"]
block = []
cook = []

[dependencies]
serialize = { package = "serialize-official", path = "` + sibling(t, x, "SERIALIZE_RS", "serialize.rs") + `" }
`
	write(t, filepath.Join(pkg, "Cargo.toml"), []byte(manifest))
	env := []string{"CARGO_TARGET_DIR=" + filepath.Join(x.repo, "build", "roundtrip-rust"), "RUSTFLAGS=-A warnings"}
	run(t, pkg, env, tool("CARGO", "cargo"), "build", "--quiet", "--offline")
	return run(t, pkg, nil, filepath.Join(x.repo, "build", "roundtrip-rust", "debug", "roundtripprobe"), x.in)
}

// runC compiles the rendered probe with every generated source and runs it.
func runC(t *testing.T, x *rowEnv) []byte {
	var headers []string
	for _, f := range genFiles(t, x.gen, ".h") {
		if strings.HasSuffix(f, "Table") {
			headers = append(headers, f+".h")
		}
	}
	probe := filepath.Join(x.work, "probe.c")
	write(t, probe, renderProbe(t, x, "c", "probe.c.tmpl", map[string]any{"Headers": headers}))
	args := []string{"-std=c11", "-O1", "-w", "-I", x.gen, "-o", filepath.Join(x.work, "probe"), probe}
	for _, f := range genFiles(t, x.gen, ".c") {
		args = append(args, filepath.Join(x.gen, f+".c"))
	}
	run(t, x.work, nil, tool("CC", "cc"), append(args, "-lm")...)
	return run(t, x.work, nil, filepath.Join(x.work, "probe"), x.in)
}

// runCPP compiles the rendered probe with every generated source and runs it.
func runCPP(t *testing.T, x *rowEnv) []byte {
	var headers []string
	for _, f := range genFiles(t, x.gen, ".h") {
		if strings.HasSuffix(f, "Table") {
			headers = append(headers, f+".h")
		}
	}
	probe := filepath.Join(x.work, "probe.cpp")
	write(t, probe, renderProbe(t, x, "cpp", "probe.cpp.tmpl", map[string]any{"Headers": headers}))
	args := []string{"-std=c++17", "-O1", "-w", "-I", x.gen, "-o", filepath.Join(x.work, "probe"), probe}
	for _, f := range genFiles(t, x.gen, ".cpp") {
		args = append(args, filepath.Join(x.gen, f+".cpp"))
	}
	run(t, x.work, nil, tool("CXX", "c++"), args...)
	return run(t, x.work, nil, filepath.Join(x.work, "probe"), x.in)
}
