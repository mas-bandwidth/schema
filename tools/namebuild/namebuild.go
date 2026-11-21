// Package namebuild is the name-buildability nightly (SPEC §4.6, File-name
// safety and Platform-name safety). A unit `schema check` accepts generates
// code that builds and loads in all 18 targets (Rowan's ruling, 2026-11-21:
// strict wins, the same in every language). testdata/name-buildability holds
// the name-shadowing survey's probe cases, one pair of files per case, and
// cases.tsv says what each takes:
//
//   - refuse-at-check: the checker refuses it for every target
//     (internal/check, TestNameBuildabilityCorpus, in the fast lane);
//   - builds: the checker accepts it, and every target generates it and
//     builds and loads the output with its own toolchain (this package);
//   - refuse-at-generate: the checker accepts it, the --lang spellings the
//     row names refuse it at generate, and every other target builds it.
//
// TestNameBuildability is the nightly: under SCHEMA_SLOW=1 it builds the
// compiler, generates every builds and refuse-at-generate row in every
// target, and builds and loads each output, every child through
// internal/testproc. Its last line is the per-target summary.
//
//	SCHEMA_SLOW=1 go test -count=1 -timeout=0 -parallel 4 ./tools/namebuild
//
// The toolchains are read from the environment under the make fragments'
// names (CC, CXX, DOTNET, DART, CARGO, GO, JAVAC, JAVA, NODE, PYTHON, RUBY,
// ELIXIR, LUA, LUAU, LUAU_ANALYZE, MOO, MOO_ANALYZE, SQ, LOBSTER,
// GOOSE), defaulting to the bare name on PATH; the serialize runtimes are the
// sibling checkouts beside the repository (../serialize, ../serialize.c,
// ../serialize.cs, ../serialize.go, ../serialize.rs), or SERIALIZE,
// SERIALIZE_C, SERIALIZE_CS, SERIALIZE_GO and SERIALIZE_RS.
package namebuild

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Targets are the 18 --lang spellings, one per target: 17 languages, with
// C# twice (modern and unity).
var Targets = []string{
	"c", "cpp", "cs", "unity", "dart", "rust", "go", "java", "js",
	"python", "ruby", "elixir", "lua", "luau", "moo", "squirrel", "goose", "lobster",
}

// Verdicts a row of cases.tsv takes.
const (
	RefuseAtCheck    = "refuse-at-check"
	Builds           = "builds"
	RefuseAtGenerate = "refuse-at-generate"
)

// Row is one line of cases.tsv.
type Row struct {
	Case    string   // the case directory under the corpus
	Verdict string   // RefuseAtCheck, Builds or RefuseAtGenerate
	Names   []string // a refuse-at-generate row's refusing targets; a refuse-at-check row's named target
	Note    string
}

// ReadRows reads dir/cases.tsv.
func ReadRows(dir string) ([]Row, error) {
	f, err := os.Open(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []Row
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 4 {
			return nil, fmt.Errorf("cases.tsv:%d: %d columns, want 4 (case, verdict, names, note)", n, len(col))
		}
		r := Row{Case: col[0], Verdict: col[1], Note: col[3]}
		if col[2] != "-" {
			r.Names = strings.Split(col[2], ",")
		}
		switch r.Verdict {
		case RefuseAtCheck, Builds:
		case RefuseAtGenerate:
			if len(r.Names) == 0 {
				return nil, fmt.Errorf("cases.tsv:%d: %s names no target", n, r.Verdict)
			}
			for _, t := range r.Names {
				if !slices.Contains(Targets, t) {
					return nil, fmt.Errorf("cases.tsv:%d: %q is not a target", n, t)
				}
			}
		default:
			return nil, fmt.Errorf("cases.tsv:%d: unknown verdict %q", n, r.Verdict)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// Job is one (case, target) the nightly runs: generate, then build and load
// unless the row expects the target to refuse at generate.
type Job struct {
	Case, Target string
	Refuse       bool // generation must refuse
}

// Plan returns every job of rows, in row then target order. A
// refuse-at-check row has none: the checker refuses it for every target.
func Plan(rows []Row) []Job {
	var jobs []Job
	for _, r := range rows {
		if r.Verdict == RefuseAtCheck {
			continue
		}
		for _, t := range Targets {
			jobs = append(jobs, Job{Case: r.Case, Target: t, Refuse: r.Verdict == RefuseAtGenerate && slices.Contains(r.Names, t)})
		}
	}
	return jobs
}

// Runner runs one child in dir with env added to the environment and returns
// its combined output and its error.
type Runner func(dir string, env []string, name string, args ...string) (string, error)

// Env is what a build reads: the toolchains, the runtimes and the
// repository root (for the Unity dialect props and the Squirrel runtime).
type Env struct {
	Root string
	Tool func(name, def string) string // a toolchain by its make variable
	// the serialize runtimes
	Serialize, SerializeC, SerializeCS, SerializeGo, SerializeRS string
}

// Case is a generated case: its basenames (Other last, which holds Holder)
// and its package.
type Case struct {
	Bases []string
	Pkg   string
}

// ReadCase reads a case directory's basenames and package.
func ReadCase(dir string) (Case, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return Case{}, err
	}
	var c Case
	for _, e := range ents {
		base, ok := strings.CutSuffix(e.Name(), ".schema")
		if !ok {
			continue
		}
		if base != "Other" {
			c.Bases = append(c.Bases, base)
		}
		if c.Pkg == "" {
			src, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return Case{}, err
			}
			first, _, _ := strings.Cut(string(src), "\n")
			c.Pkg = strings.TrimSpace(strings.TrimPrefix(first, "package "))
		}
	}
	sort.Strings(c.Bases)
	c.Bases = append(c.Bases, "Other")
	return c, nil
}

func capital(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func put(dir, name, body string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}

func glob(dir, pat string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, pat))
	sort.Strings(m)
	return m
}

func names(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

// Build builds and loads target's generated output in dir (a private copy
// the build may write into) with the target's own toolchain, and returns nil
// or the failure with its output.
func Build(run Runner, env Env, target string, c Case, dir string) error {
	step := func(name string, args ...string) error {
		return stepEnv(run, dir, nil, name, args...)
	}
	switch target {
	case "c":
		// the data headers first, then the wire headers that include them
		var sb strings.Builder
		for _, h := range glob(dir, "*.h") {
			if !strings.HasSuffix(h, "Wire.h") {
				fmt.Fprintf(&sb, "#include %q\n", filepath.Base(h))
			}
		}
		for _, h := range glob(dir, "*Wire.h") {
			fmt.Fprintf(&sb, "#include %q\n", filepath.Base(h))
		}
		sb.WriteString("int main(void) { return 0; }\n")
		if err := put(dir, "zz_main.c", sb.String()); err != nil {
			return err
		}
		args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I" + env.SerializeC, "-I" + dir}
		args = append(args, glob(dir, "*.c")...)
		args = append(args, filepath.Join(env.SerializeC, "serialize.c"), "-lm", "-o", filepath.Join(dir, "zz_main"))
		if err := step(env.Tool("CC", "gcc"), args...); err != nil {
			return err
		}
		return step(filepath.Join(dir, "zz_main"))
	case "cpp":
		var sb strings.Builder
		for _, h := range glob(dir, "*.h") {
			fmt.Fprintf(&sb, "#include %q\n", filepath.Base(h))
		}
		sb.WriteString("int main() { return 0; }\n")
		if err := put(dir, "zz_main.cpp", sb.String()); err != nil {
			return err
		}
		args := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-ffp-contract=off", "-I" + env.Serialize, "-I" + dir}
		args = append(args, names(glob(dir, "*.cpp"))...) // zz_main.cpp and every generated translation unit
		args = append(args, "-o", "zz_main")
		if err := step(env.Tool("CXX", "g++"), args...); err != nil {
			return err
		}
		return step(filepath.Join(dir, "zz_main"))
	case "cs":
		proj := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Library</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="` + env.SerializeCS + `/src/Serialize.cs" />
    <Compile Include="` + env.SerializeCS + `/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`
		if err := put(dir, "zz.csproj", proj); err != nil {
			return err
		}
		return stepEnv(run, dir, []string{"SerializeCsRoot=" + env.SerializeCS}, env.Tool("DOTNET", "dotnet"), "build", "zz.csproj", "--nologo", "-v", "quiet")
	case "unity":
		proj := `<Project Sdk="Microsoft.NET.Sdk">
  <Import Project="` + env.Root + `/test/unity/UnityDialect.props" />
  <PropertyGroup>
    <TargetFramework>netstandard2.1</TargetFramework>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
  </ItemGroup>
</Project>
`
		if err := put(dir, "zz.csproj", proj); err != nil {
			return err
		}
		return stepEnv(run, dir, []string{"SerializeCsRoot=" + env.SerializeCS}, env.Tool("DOTNET", "dotnet"), "build", "zz.csproj", "--nologo", "-v", "quiet")
	case "dart":
		var sb strings.Builder
		sb.WriteString("// ignore_for_file: unused_import\n")
		for i, b := range c.Bases {
			fmt.Fprintf(&sb, "import '%s.dart' as m%d;\n", b, i)
		}
		fmt.Fprintf(&sb, "void main() { final h = m%d.Holder(); print(h.runtimeType); }\n", len(c.Bases)-1)
		if err := put(dir, "zz_main.dart", sb.String()); err != nil {
			return err
		}
		if err := step(env.Tool("DART", "dart"), "analyze", "."); err != nil {
			return err
		}
		return step(env.Tool("DART", "dart"), "--enable-asserts", "zz_main.dart")
	case "rust":
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
			return err
		}
		for _, f := range glob(dir, "*.rs") {
			if err := os.Rename(f, filepath.Join(dir, "src", filepath.Base(f))); err != nil {
				return err
			}
		}
		toml := "[package]\nname = \"probecrate\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nserialize = { package = \"serialize-official\", path = \"" + env.SerializeRS + "\" }\n"
		if err := put(dir, "Cargo.toml", toml); err != nil {
			return err
		}
		return stepEnv(run, dir, []string{"CARGO_TARGET_DIR=" + filepath.Join(dir, "target")}, env.Tool("CARGO", "cargo"), "build", "--quiet", "--offline")
	case "go":
		mod := "module probecase\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => " + env.SerializeGo + "\n"
		if err := put(dir, "go.mod", mod); err != nil {
			return err
		}
		goEnv := []string{"GOFLAGS=-mod=mod", "GOWORK=off"}
		if err := stepEnv(run, dir, goEnv, env.Tool("GO", "go"), "build", "./..."); err != nil {
			return err
		}
		return stepEnv(run, dir, goEnv, env.Tool("GO", "go"), "vet", "./...")
	case "java":
		var sb strings.Builder
		p, o := c.Pkg, "Other"
		fmt.Fprintf(&sb, "public final class ZzMain {\n  public static void main(String[] args) {\n")
		fmt.Fprintf(&sb, "    %s.%s.Holder h = new %s.%s.Holder();\n", p, o, p, o)
		fmt.Fprintf(&sb, "    %s.%s.initHolder(h);\n", p, o)
		fmt.Fprintf(&sb, "    byte[] d = new byte[%s.%s.HOLDER_MAX_BYTES];\n", p, o)
		fmt.Fprintf(&sb, "    if (%s.%s.writeHolder(h, d) <= 0) throw new AssertionError(\"FAILED: write\");\n", p, o)
		sb.WriteString("    System.out.println(\"ok\");\n  }\n}\n")
		if err := put(dir, "ZzMain.java", sb.String()); err != nil {
			return err
		}
		args := []string{"--release", "17", "-Xlint:all", "-Werror", "-d", "classes"}
		args = append(args, names(glob(dir, "*.java"))...)
		if err := step(env.Tool("JAVAC", "javac"), args...); err != nil {
			return err
		}
		return step(env.Tool("JAVA", "java"), "-ea", "-cp", "classes", "ZzMain")
	case "js":
		var sb strings.Builder
		for i, f := range glob(dir, "*.js") {
			fmt.Fprintf(&sb, "import * as m%d from %q;\n", i, "./"+filepath.Base(f))
		}
		sb.WriteString("import * as o from \"./Other.js\";\nconst h = new o.Holder();\no.InitHolder(h);\nconsole.log(\"ok\");\n")
		if err := put(dir, "zz_main.mjs", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("NODE", "node"), "zz_main.mjs")
	case "python":
		var sb strings.Builder
		// -I keeps the user's site and environment out, so the package's
		// directory goes on the path here
		fmt.Fprintf(&sb, "import importlib, sys\nsys.path.insert(0, %q)\nm = importlib.import_module(%q)\n", dir, c.Pkg)
		for _, b := range c.Bases {
			fmt.Fprintf(&sb, "importlib.import_module(%q)\n", c.Pkg+"."+b)
		}
		sb.WriteString("H = m.Holder\nv = H()\nd = v.write()\nr = H.read(d)\nassert r.write() == d\nprint('ok')\n")
		if err := put(dir, "zz_main.py", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("PYTHON", "python3"), "-I", "zz_main.py")
	case "ruby":
		P := capital(c.Pkg)
		body := fmt.Sprintf("require_relative %q\nv = %s::Holder.new\nd = v.write\nr = %s::Holder.read(d)\nraise 'FAILED: round trip' unless r.write == d\nputs 'ok'\n", c.Pkg, P, P)
		if err := put(dir, "zz_main.rb", body); err != nil {
			return err
		}
		return step(env.Tool("RUBY", "ruby"), "-w", "zz_main.rb")
	case "elixir":
		// the support module, then the unit's files in dependency order
		// (Other, which holds Holder, last), then anything else emitted
		var order []string
		if _, err := os.Stat(filepath.Join(dir, "PacketRuntime.ex")); err == nil {
			order = append(order, "PacketRuntime.ex")
		}
		for _, b := range c.Bases {
			order = append(order, b+".ex")
		}
		for _, f := range names(glob(dir, "*.ex")) {
			if !slices.Contains(order, f) {
				order = append(order, f)
			}
		}
		var sb strings.Builder
		for _, f := range order {
			fmt.Fprintf(&sb, "Code.require_file(%q, __DIR__)\n", f)
		}
		P := capital(c.Pkg)
		fmt.Fprintf(&sb, "v = struct(%s.Holder)\nwire = %s.Other.write_holder!(v)\n{:ok, r} = %s.Other.read_holder(wire, bit_size(wire))\n^wire = %s.Other.write_holder!(r)\nIO.puts(\"ok\")\n", P, P, P, P)
		if err := put(dir, "zz_main.exs", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("ELIXIR", "elixir"), "zz_main.exs")
	case "lua":
		var sb strings.Builder
		sb.WriteString("package.path = \"./?.lua;\" .. package.path\nlocal serialize = require(\"serialize\")\n")
		for i, b := range c.Bases {
			fmt.Fprintf(&sb, "local m%d = require(%q)\nassert(type(m%d) == \"table\" and m%d ~= _G[%q], \"FAILED: require(%s) did not answer with the generated module\")\n", i, b, i, i, b, b)
		}
		o := fmt.Sprintf("m%d", len(c.Bases)-1)
		fmt.Fprintf(&sb, "local v = %s.NewHolder()\nlocal w = serialize.WriteStream.new(4096)\nassert(%s.WriteHolder(w, v), \"FAILED: write\")\nw:flush()\nlocal r = serialize.ReadStream.new(w:data())\nlocal x = %s.NewHolder()\nassert(%s.ReadHolder(r, x), \"FAILED: read\")\nprint(\"ok\")\n", o, o, o, o)
		if err := put(dir, "zz_main.lua", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("LUA", "lua"), "zz_main.lua")
	case "luau", "moo":
		ext := "." + target
		up := strings.ToUpper(target)
		var sb strings.Builder
		sb.WriteString("local serialize = require(\"./serialize\")\n")
		for i, b := range c.Bases {
			fmt.Fprintf(&sb, "local m%d = require(%q)\n", i, "./"+b)
		}
		o := fmt.Sprintf("m%d", len(c.Bases)-1)
		fmt.Fprintf(&sb, "local v = %s.NewHolder()\nlocal w = serialize.WriteStream.new(buffer.create(4096))\nassert(%s.WriteHolder(w, v), \"FAILED: write\")\nw:flush()\nlocal r = serialize.ReadStream.new(w:data())\nlocal x = %s.NewHolder()\nassert(%s.ReadHolder(r, x), \"FAILED: read\")\nprint(\"ok\")\n", o, o, o, o)
		if err := put(dir, "zz_main"+ext, sb.String()); err != nil {
			return err
		}
		var files []string
		for _, f := range glob(dir, "*"+ext) {
			if filepath.Base(f) != "zz_main"+ext {
				files = append(files, filepath.Base(f))
			}
		}
		if err := step(env.Tool(up, target), "zz_main"+ext); err != nil {
			return err
		}
		return step(env.Tool(up+"_ANALYZE", target+"-analyze"), files...)
	case "squirrel":
		var sb strings.Builder
		fmt.Fprintf(&sb, "local serialize = dofile(%q, true);\nlocal mod = dofile(%q, true);\n",
			filepath.Join(env.Root, "internal/codegen/squirrel/serialize/serialize.nut"), filepath.Join(dir, "schema_module.nut"))
		for i, b := range c.Bases {
			fmt.Fprintf(&sb, "local m%d = mod.require(%q);\n", i, b)
		}
		o := fmt.Sprintf("m%d", len(c.Bases)-1)
		fmt.Fprintf(&sb, "local v = %s.Holder();\nlocal w = serialize.WriteStream(blob(4096));\nif (!%s.WriteHolder(w, v)) throw \"FAILED: write\";\nw.flush();\nlocal r = serialize.ReadStream(w.data());\nlocal x = %s.Holder();\nif (!%s.ReadHolder(r, x)) throw \"FAILED: read\";\nprint(\"ok\\n\");\n", o, o, o, o)
		if err := put(dir, "zz_main.nut", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("SQ", "sq"), "zz_main.nut")
	case "lobster":
		var sb strings.Builder
		for _, b := range c.Bases {
			fmt.Fprintf(&sb, "import .%s\n", b)
		}
		sb.WriteString("print(\"ok\")\n")
		if err := put(dir, "zz_main.lobster", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("LOBSTER", "lobster"), "zz_main.lobster")
	case "goose":
		var sb strings.Builder
		for _, b := range c.Bases {
			fmt.Fprintf(&sb, "import .%s;\n", b)
		}
		sb.WriteString("\nfn main() {\n    print(\"ok\");\n}\n")
		if err := put(dir, "zz_main.goose", sb.String()); err != nil {
			return err
		}
		return step(env.Tool("GOOSE", "goose"), "run", "zz_main.goose")
	}
	return fmt.Errorf("namebuild: no build for target %q", target)
}

// stepEnv runs one build step and turns a failure, or output that says
// FAILED, into an error carrying the output.
func stepEnv(run Runner, dir string, env []string, name string, args ...string) error {
	out, err := run(dir, env, name, args...)
	if err == nil && strings.Contains(out, "FAILED") {
		err = fmt.Errorf("the output says FAILED")
	}
	if err != nil {
		return fmt.Errorf("%s %s: %v\n%s", filepath.Base(name), strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

// FirstError returns the line of a failure that names it: the first holding
// "rror" or "FAILED", else the first line, cut to 300 bytes.
func FirstError(err error) string {
	lines := strings.Split(err.Error(), "\n")
	first := lines[0]
	for _, l := range lines {
		if strings.Contains(l, "rror") || strings.Contains(l, "FAILED") {
			first = l
			break
		}
	}
	first = strings.TrimSpace(first)
	if len(first) > 300 {
		first = first[:300]
	}
	return first
}
