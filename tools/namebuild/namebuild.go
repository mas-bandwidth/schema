// Package namebuild is the name-buildability nightly (SPEC §4.6, File-name
// safety and Platform-name safety). A unit `schema check` accepts generates
// code that builds and loads in every target (Rowan's ruling, 2026-11-21:
// strict wins, the same in every language). testdata/name-buildability holds
// the name-shadowing survey's probe cases, one pair of files per case, and
// cases.tsv says what each takes:
//
//   - refuse-at-check: the checker refuses it for every target
//     (internal/check, TestNameBuildabilityCorpus, in the fast lane);
//   - builds: the checker accepts it, and every target generates it and
//     builds and loads the output with its own toolchain (this package).
//
// There is no third verdict: a name the checker accepts and an emitter
// refuses at generate is a defect, and the checker runs the emitters' name
// rules so it refuses such a name itself.
//
// A builds row a target cannot build yet fails, by name, until it builds:
// there is no list of expected failures (Glenn, 2026-11-25: no skip and no
// exemption list of any kind).
//
// The nightly is one test per target, TestNameBuildability_<target>, with a
// subtest per builds row named by its case: each subtest is one registry
// instance, <lang>.nightly.codegen.name-buildability.<case> (RegistryRows),
// and schema-test runs it by that name. Each builds the compiler once per
// run, generates the row in its target, and builds and loads the output,
// every child through internal/testproc. The run's last lines are the
// per-target summary.
//
//	go test -count=1 -timeout=0 -parallel 4 -run '^TestNameBuildability_' ./tools/namebuild
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
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// Targets are the --lang spellings, one per generator, with C# twice
// (modern and unity); TestTargetsAreTheGenerators holds them to the
// generators the compiler registers.
var Targets = []string{
	"c", "cpp", "cs", "unity", "dart", "rust", "go", "java", "js",
	"python", "ruby", "elixir", "lua", "luau", "moo", "squirrel", "goose", "lobster",
}

// Verdicts a row of cases.tsv takes.
const (
	RefuseAtCheck = "refuse-at-check"
	Builds        = "builds"
)

// Row is one line of cases.tsv.
type Row struct {
	Case    string   // the case directory under the corpus
	Verdict string   // RefuseAtCheck or Builds
	Names   []string // a refuse-at-check row's named target
	Note    string
	Line    int // its line in cases.tsv
}

// caseName is what go test keeps unchanged as a subtest name: printable
// ASCII with no space and no "/". A case is a subtest, and go test rewrites
// a space, splits at "/" and suffixes a repeat, so a name outside it would
// never match its registry row's subtest.
var caseName = regexp.MustCompile(`^[\x21-\x2e\x30-\x7e]+$`)

// ReadRows reads dir/cases.tsv. A case name go test would rewrite, and a
// case named twice, are refused by line.
func ReadRows(dir string) ([]Row, error) {
	f, err := os.Open(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: a close error loses nothing
	var rows []Row
	seen := map[string]int{}
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
		r := Row{Case: col[0], Verdict: col[1], Note: col[3], Line: n}
		if !caseName.MatchString(r.Case) {
			return nil, fmt.Errorf("cases.tsv:%d: case %q holds a space, a \"/\" or a byte outside printable ASCII (go test would rename it as a subtest)", n, r.Case)
		}
		if prev, dup := seen[r.Case]; dup {
			return nil, fmt.Errorf("cases.tsv:%d: case %s again (first at line %d)", n, r.Case, prev)
		}
		seen[r.Case] = n
		if col[2] != "-" {
			r.Names = strings.Split(col[2], ",")
		}
		switch r.Verdict {
		case RefuseAtCheck, Builds:
		default:
			return nil, fmt.Errorf("cases.tsv:%d: unknown verdict %q", n, r.Verdict)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// Job is one (case, target) the nightly runs: generate, then build and load.
type Job struct {
	Case, Target string
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
			jobs = append(jobs, Job{Case: r.Case, Target: t})
		}
	}
	return jobs
}

// THE REGISTRY ROWS (docs/TEST-REGISTRY.md). Each builds row of cases.tsv is
// one per-language test of the registry, codegen.name-buildability.<case>,
// tier nightly; its instance for a language is the subtest <case> of
// TestNameBuildability_<lang>, which schema-test's corpus adapter runs by
// that name (tools/schema-test, nameBuildRun).

// Family is the registry family of the name-buildability rows.
const Family = "name-buildability"

// Package is the Go package the rows' tests are in, as go test names it.
const Package = "./tools/namebuild"

// TestPrefix starts every per-target test's name.
const TestPrefix = "TestNameBuildability_"

// LangPlaceholder stands for the language in a row's inputs: one row serves
// every language.
const LangPlaceholder = testreg.LangPlaceholder

// TestName is the per-target test of target.
func TestName(target string) string { return TestPrefix + target }

// Inputs are a name-buildability row's inputs: the Go test (with
// LangPlaceholder for the language) and its subtest, the case.
type Inputs struct {
	Package string `json:"package"`
	Test    string `json:"test"`
	Subtest string `json:"subtest"`
}

// RegistryCase spells a case directory as a registry case: lower case, with
// "_" as "-", and a trailing "_" (a probe of a name with a trailing
// underscore) as "-tail", since a registry case cannot end in "-".
func RegistryCase(c string) string {
	s := c
	if t, ok := strings.CutSuffix(s, "_"); ok {
		s = t + "-tail"
	}
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.TrimPrefix(s, "-")
}

type registryRow struct {
	Rec    string `json:"rec"`
	Key    string `json:"key"`
	Type   string `json:"type"`
	Family string `json:"family"`
	Case   string `json:"case"`
	Tier   string `json:"tier"`
	Checks string `json:"checks"`
	Inputs Inputs `json:"inputs"`
	Expect string `json:"expect"`
	Source string `json:"source"`
}

// RegistryRows are the registry lines of rows' builds rows, in cases.tsv
// order. Two cases that spell one registry case are refused, naming both.
func RegistryRows(rows []Row) ([]string, error) {
	seen := map[string]string{}
	var out []string
	for _, r := range rows {
		if r.Verdict != Builds {
			continue
		}
		rc := RegistryCase(r.Case)
		if prev, dup := seen[rc]; dup {
			return nil, fmt.Errorf("cases %s and %s are both the registry case %s", prev, r.Case, rc)
		}
		seen[rc] = r.Case
		b, err := json.Marshal(registryRow{
			Rec: "test", Key: "codegen." + Family + "." + rc, Type: "codegen", Family: Family, Case: rc, Tier: "nightly",
			Checks: "name-buildability " + r.Case + ": the checker accepts its names, and the language generates it and builds and loads the output with its own toolchain (SPEC §4.6)",
			Inputs: Inputs{Package: Package, Test: TestPrefix + LangPlaceholder, Subtest: r.Case},
			Expect: "the Go test " + TestPrefix + "<lang>/" + r.Case + " passes: generate, build and load all succeed",
			Source: fmt.Sprintf("testdata/name-buildability/cases.tsv:%d", r.Line),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, string(b))
	}
	return out, nil
}

// SplitRegistry splits registry text into the lines that are not
// name-buildability rows (as text, each with its newline) and those that are.
func SplitRegistry(src string) (kept string, rows []string) {
	var sb strings.Builder
	for line := range strings.Lines(src) {
		body := strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(body, `{"rec":"test",`) && strings.Contains(body, `,"family":"`+Family+`",`) {
			rows = append(rows, body)
			continue
		}
		sb.WriteString(line)
	}
	return sb.String(), rows
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
	matches, _ := fs.Glob(os.DirFS(dir), pat) // errdiscard: callers supply valid, fixed filename patterns; fs.Glob only returns ErrBadPattern.
	for i, name := range matches {
		matches[i] = filepath.Join(dir, name)
	}
	return matches
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
    <!-- a unit with tables: the table backend's spans and pinned buffers are
         unsafe code, which docs/CS-TABLES.md asks every consumer to allow -->
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
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
		// a unit with tables imports the shared table runtime
		// (github.com/mas-bandwidth/schema/v2/tables/goruntime), so this
		// checkout's module rides beside serialize.go, as the gotable tests'
		// own probe modules do
		mod := "module probecase\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\n" +
			"replace github.com/mas-bandwidth/serialize.go/v2 => " + env.SerializeGo + "\nreplace github.com/mas-bandwidth/schema/v2 => " + env.Root + "\n"
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
		return fmt.Errorf("%s %s: %w\n%s", filepath.Base(name), strings.Join(args, " "), err, strings.TrimSpace(out))
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
