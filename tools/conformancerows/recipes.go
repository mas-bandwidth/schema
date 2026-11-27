package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// runLogged runs argv from the repository root and appends its output to the
// row's log. A failure carries the command and the output's tail.
func (c *ctx) runLogged(row string, extraEnv []string, argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = c.root
	cmd.Env = append(os.Environ(), extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	logPath := filepath.Join(c.work, row+".log")
	f, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if ferr == nil {
		_, werr := fmt.Fprintf(f, "$ %s\n%s\n", strings.Join(argv, " "), out.Bytes())
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil && err == nil {
			err = fmt.Errorf("writing the row's log %s: %w", logPath, werr)
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", strings.Join(argv, " "), err, tail(out.String(), 30))
	}
	return nil
}

// runProbe runs argv from the repository root for its exit status alone:
// nothing it prints reaches the row's log.
func (c *ctx) runProbe(argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = c.root
	return cmd.Run()
}

// runShell runs a make-supplied command string (a toolchain variable may be
// a command with its own arguments) followed by quoted arguments.
func (c *ctx) runShell(row, command string, args ...string) error {
	var line strings.Builder
	line.WriteString(command)
	for _, a := range args {
		line.WriteString(" " + strconv.Quote(a))
	}
	return c.runLogged(row, nil, "sh", "-c", line.String())
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (c *ctx) resetLog(row string) {
	if err := os.Remove(filepath.Join(c.work, row+".log")); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "conformancerows: %v\n", err)
	}
}

// ---- C and C++: the row's #include lines name what it builds against ----

var includeRe = regexp.MustCompile(`(?m)^\s*#\s*include\s+"([^"]+)"`)

// fileIndex maps a file's base name to its path under the generated roots,
// the first root (in the order given) winning a name two roots hold.
type fileIndex struct {
	once  sync.Once
	paths map[string]string
	err   error
}

var indexes sync.Map // leg -> *fileIndex

func (c *ctx) index(roots []string) (map[string]string, error) {
	v, _ := indexes.LoadOrStore(c.leg, &fileIndex{})
	ix := v.(*fileIndex)
	ix.once.Do(func() {
		ix.paths = map[string]string{}
		for _, r := range roots {
			dir := filepath.Join(c.root, r)
			if _, err := os.Stat(dir); err != nil {
				ix.err = fmt.Errorf("the generated tree %s is missing: %w", r, err)
				return
			}
			var names []string
			found := map[string]string{}
			if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					if _, seen := found[d.Name()]; !seen {
						found[d.Name()] = p
						names = append(names, d.Name())
					}
				}
				return nil
			}); err != nil {
				ix.err = err
				return
			}
			sort.Strings(names)
			for _, n := range names {
				if _, held := ix.paths[n]; !held {
					ix.paths[n] = found[n]
				}
			}
		}
	})
	return ix.paths, ix.err
}

// resolve reads the row's quoted includes and returns the include
// directories they live in and the generated sources to compile beside the
// row: every source (ext) in the directory of each generated header the row
// names, except a source the row #includes itself.
func (c *ctx) resolve(src string, roots []string, ext string) (incs, srcs []string, err error) {
	text, err := os.ReadFile(src)
	if err != nil {
		return nil, nil, err
	}
	idx, err := c.index(roots)
	if err != nil {
		return nil, nil, err
	}
	seenDir := map[string]bool{}
	included := map[string]bool{}
	var unitDirs []string
	for _, m := range includeRe.FindAllStringSubmatch(string(text), -1) {
		p, ok := idx[filepath.Base(m[1])]
		if !ok {
			continue // a runtime or system header the -I list below holds
		}
		included[p] = true
		dir := filepath.Dir(p)
		if !seenDir[dir] {
			seenDir[dir] = true
			incs = append(incs, dir)
			unitDirs = append(unitDirs, dir)
		}
	}
	for _, dir := range unitDirs {
		ents, rerr := os.ReadDir(dir)
		if rerr != nil {
			return nil, nil, rerr
		}
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			if !e.IsDir() && strings.HasSuffix(e.Name(), ext) && !included[p] {
				srcs = append(srcs, p)
			}
		}
	}
	return incs, srcs, nil
}

var cRoots = []string{"build/tables-generated-c", "build/tables-generated-c-fixed", "test/conformance/c"}

// roots is cRoots, or with -gen the one narrow tree and the driver's unit
// sources.
func (c *ctx) cRoots() []string {
	if c.gen != "" {
		return []string{c.gen, "test/conformance/c"}
	}
	return cRoots
}

var cppRoots = []string{"build/tables-generated"}

func rowC(c *ctx, row string) error {
	cc, err := env("CC")
	if err != nil {
		return err
	}
	runtime, err := env("SERIALIZE_C")
	if err != nil {
		return err
	}
	// _POSIX_C_SOURCE: a generated table unit that formats floats uses
	// locale_t, which glibc declares only under POSIX.1-2008, and the unit
	// says so with an #error unless the translation unit defines it before
	// its first #include (internal/codegen/ctable). The generated sources
	// define it at their top; a row is a translation unit of its own that
	// includes the header, so the recipe defines it for every row.
	flags := []string{"-std=c11", "-D_POSIX_C_SOURCE=200809L", "-Wall", "-Wextra", "-Werror", "-ffp-contract=off"}
	extra := []string{"-I" + runtime, "-I" + filepath.Join(c.root, "test/conformance/c")}
	return c.rowNative(row, ".c", c.cRoots(), strings.Fields(cc), flags, extra, []string{"-lm"})
}

func rowCpp(c *ctx, row string) error {
	cxx, err := env("CXX")
	if err != nil {
		return err
	}
	runtime, err := env("SERIALIZE")
	if err != nil {
		return err
	}
	flags := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-ffp-contract=off", "-pthread"}
	return c.rowNative(row, ".cpp", cppRoots, strings.Fields(cxx), flags, []string{"-I" + runtime}, []string{"-pthread"})
}

// rowNative compiles the row against the generated headers it includes and
// links it on its own; only a row whose link needs the units' compiled
// sources (an undefined reference) is linked again with them, each unit
// source compiled once per run and shared by every row that needs it. The
// first link is a probe: its output stays out of the row's log, so a row that
// links with the units and then fails is named by its own failure, never by
// the probe's undefined reference.
func (c *ctx) rowNative(row, ext string, roots, compiler, flags, extra, libs []string) error {
	c.resetLog(row)
	src := c.rowPath(row, ext)
	incs, srcs, err := c.resolve(src, roots, ext)
	if err != nil {
		return err
	}
	var inc []string
	for _, d := range incs {
		inc = append(inc, "-I"+d)
	}
	obj := filepath.Join(c.work, row+".o")
	argv := append(append(append(append([]string{}, compiler...), flags...), inc...), extra...)
	if err := c.runLogged(row, nil, append(argv, "-c", src, "-o", obj)...); err != nil {
		return fmt.Errorf("does not build: %w", err)
	}
	out := filepath.Join(c.work, row)
	link := append(append([]string{}, compiler...), flags...)
	if err := c.runProbe(append(append(link, obj, "-o", out), libs...)...); err != nil {
		var objs []string
		for _, s := range srcs {
			o, uerr := c.unitObject(s, compiler, flags, extra)
			if uerr != nil {
				return fmt.Errorf("does not build: the generated source it links: %w", uerr)
			}
			objs = append(objs, o)
		}
		argv := append(append(link, obj), objs...)
		if err := c.runLogged(row, nil, append(append(argv, "-o", out), libs...)...); err != nil {
			return fmt.Errorf("does not build: %w", err)
		}
	}
	return c.runLogged(row, nil, out)
}

type unitObj struct {
	once sync.Once
	obj  string
	err  error
}

var unitObjs sync.Map // source path -> *unitObj

// unitObject compiles one generated source once per run.
func (c *ctx) unitObject(src string, compiler, flags, extra []string) (string, error) {
	v, _ := unitObjs.LoadOrStore(src, &unitObj{})
	u := v.(*unitObj)
	u.once.Do(func() {
		dir := filepath.Join(c.work, "units")
		if u.err = os.MkdirAll(dir, 0o755); u.err != nil {
			return
		}
		rel, err := filepath.Rel(c.root, src)
		if err != nil {
			u.err = err
			return
		}
		u.obj = filepath.Join(dir, strings.ReplaceAll(rel, string(filepath.Separator), "_")+".o")
		argv := append(append(append(append([]string{}, compiler...), flags...), "-I"+filepath.Dir(src)), extra...)
		cmd := exec.Command(argv[0], append(argv[1:], "-c", src, "-o", u.obj)...)
		cmd.Dir = c.root
		if out, err := cmd.CombinedOutput(); err != nil {
			u.err = fmt.Errorf("%s: %w\n%s", rel, err, tail(string(out), 30))
		}
	})
	return u.obj, u.err
}

// ---- the scripted legs: the toolchain runs the row file itself ----

func rowDart(c *ctx, row string) error {
	c.resetLog(row)
	dart, err := env("DART")
	if err != nil {
		return err
	}
	return c.runShell(row, dart+" --enable-asserts run", c.rowPath(row, ".dart"))
}

// ---- Java: the packages a row names are the generated units it runs on ----

var (
	javaPackageRe = regexp.MustCompile(`(?m)^\s*package\s+([a-z][a-z0-9_]*)\s*;`)
	javaRefRe     = regexp.MustCompile(`\b([a-z][a-z0-9_]*)\.(?:[A-Z*])`)
)

// prepareJava reads the generated Java tree's units (each directory under
// build/tables-generated-java and the one package its files declare), finds
// the packages each row of the shard names, and compiles each named unit
// once, on its own, into build/conformance-rows/java/units/<unit>. A row's
// classpath is the units it names and nothing else, so a row runs on any
// generated unit without a hand list of what the leg's driver compiles. A
// unit that does not compile is the build failure of every row naming it.
func prepareJava(c *ctx, rows []string) error {
	javac, err := env("JAVAC")
	if err != nil {
		return err
	}
	tree := c.genTree(filepath.Join("build", "tables-generated-java"))
	genDir := filepath.Join(c.root, tree)
	ents, err := os.ReadDir(genDir)
	if err != nil {
		return fmt.Errorf("the generated tree %s is missing: %w", tree, err)
	}
	unitOf := map[string]string{} // package -> unit directory (the first by name)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(genDir, e.Name(), "*.java"))
		if len(files) == 0 {
			continue
		}
		text, rerr := os.ReadFile(files[0])
		if rerr != nil {
			return rerr
		}
		if m := javaPackageRe.FindSubmatch(text); m != nil {
			if _, held := unitOf[string(m[1])]; !held {
				unitOf[string(m[1])] = e.Name()
			}
		}
	}
	c.javaCP = map[string]string{}
	need := map[string][]string{} // unit -> rows naming it
	for _, row := range rows {
		text, rerr := os.ReadFile(c.rowPath(row, ".java"))
		if rerr != nil {
			return rerr
		}
		var cp []string
		seen := map[string]bool{}
		for _, m := range javaRefRe.FindAllStringSubmatch(string(text), -1) {
			if u, ok := unitOf[m[1]]; ok && !seen[u] {
				seen[u] = true
				cp = append(cp, filepath.Join(c.work, "units", u))
				need[u] = append(need[u], row)
			}
		}
		sort.Strings(cp)
		c.javaCP[row] = strings.Join(cp, string(os.PathListSeparator))
	}
	var units []string
	for u := range need {
		units = append(units, u)
	}
	sort.Strings(units)
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	var mu sync.Mutex
	for _, u := range units {
		wg.Add(1)
		sem <- struct{}{}
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			out := filepath.Join(c.work, "units", u)
			srcs, _ := filepath.Glob(filepath.Join(genDir, u, "*.java"))
			var line strings.Builder
			line.WriteString("rm -rf " + strconv.Quote(out) + " && mkdir -p " + strconv.Quote(out) + " && " + javac + " --release 17 -nowarn -d " + strconv.Quote(out))
			for _, s := range srcs {
				line.WriteString(" " + strconv.Quote(s))
			}
			cmd := exec.Command("sh", "-c", line.String())
			cmd.Dir = c.root
			if b, cerr := cmd.CombinedOutput(); cerr != nil {
				mu.Lock()
				for _, row := range need[u] {
					c.pre[row] = fmt.Errorf("does not build: the generated unit %s/%s it names: %w\n%s", tree, u, cerr, tail(string(b), 30))
				}
				mu.Unlock()
			}
		}(u)
	}
	wg.Wait()
	return nil
}

func rowJava(c *ctx, row string) error {
	c.resetLog(row)
	java, err := env("JAVA")
	if err != nil {
		return err
	}
	cp := c.javaCP[row]
	if cp == "" {
		cp = "."
	}
	return c.runShell(row, java+" -ea -cp "+strconv.Quote(cp), c.rowPath(row, ".java"))
}

func rowJs(c *ctx, row string) error {
	c.resetLog(row)
	node, err := env("NODE")
	if err != nil {
		return err
	}
	return c.runShell(row, node, c.rowPath(row, ".mjs"))
}

func rowElixir(c *ctx, row string) error {
	c.resetLog(row)
	elixir, err := env("ELIXIR")
	if err != nil {
		return err
	}
	return c.runShell(row, elixir+" -pa build/elixir-tables-ebin -pa build/elixir-fixedform/ebin", c.rowPath(row, ".exs"))
}

func rowCs(c *ctx, row string) error {
	c.resetLog(row)
	dotnet, err := env("DOTNET")
	if err != nil {
		return err
	}
	runtime, err := env("SERIALIZE_CS")
	if err != nil {
		return err
	}
	// The rows directory's Directory.Build.targets compiles the serialize.cs
	// runtime beside a row's generated units when it is given, as every
	// other C# build of generated tables does (make/cs.mk SERIALIZE_CS).
	project := filepath.Join(c.rowsDir, row)
	if err := c.runShell(row, dotnet+" build --nologo -v q -p:SerializeCsSrc="+strconv.Quote(filepath.Join(runtime, "src")), project); err != nil {
		return fmt.Errorf("does not build: %w", err)
	}
	return c.runShell(row, dotnet+" run --no-build --project", project)
}

// ---- Rust: every row is an integration test of one probe package ----

var (
	rustCrateRe = regexp.MustCompile(`(?:extern\s+crate\s+([a-z][a-z0-9_]*)|\b([a-z][a-z0-9_]*)::)`)
	rustModRe   = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?mod\s+([a-z][a-z0-9_]*)\s*;`)
)

// prepareRust writes one probe package whose tests are the shard's row files
// in place (so a row's #[path] stays relative to the rows directory) and
// whose dependencies are the generated crates the rows name, builds every
// test with --keep-going, and records each row's executable or build failure.
func prepareRust(c *ctx, rows []string) error {
	cargo, err := env("CARGO")
	if err != nil {
		return err
	}
	tree := c.genTree(filepath.Join("build", "tables-generated-rust"))
	genDir := filepath.Join(c.root, tree)
	ents, err := os.ReadDir(genDir)
	if err != nil {
		return fmt.Errorf("the generated tree %s is missing: %w", tree, err)
	}
	crates := map[string]bool{}
	for _, e := range ents {
		if _, serr := os.Stat(filepath.Join(genDir, e.Name(), "Cargo.toml")); e.IsDir() && serr == nil {
			crates[e.Name()] = true
		}
	}
	// A row that includes a generated file as a LOCAL module (#[path] ... mod
	// tblfu1;) names it like the crate; a dependency of the same name would
	// make its `use tblfu1::` ambiguous, so a name any row declares as a
	// module is never a dependency.
	deps := map[string]bool{}
	local := map[string]bool{}
	for _, row := range rows {
		text, rerr := os.ReadFile(c.rowPath(row, ".rs"))
		if rerr != nil {
			return rerr
		}
		for _, m := range rustCrateRe.FindAllStringSubmatch(string(text), -1) {
			for _, name := range m[1:] {
				if crates[name] {
					deps[name] = true
				}
			}
		}
		for _, m := range rustModRe.FindAllStringSubmatch(string(text), -1) {
			local[m[1]] = true
		}
	}
	for name := range local {
		delete(deps, name)
	}
	if err := c.rustPathUnits(rows, genDir, crates); err != nil {
		return err
	}
	probe := filepath.Join(c.work, "probe")
	if err := os.MkdirAll(filepath.Join(probe, "src"), 0o755); err != nil {
		return err
	}
	var toml strings.Builder
	toml.WriteString("# written by tools/conformancerows: one integration test per row file\n")
	toml.WriteString("[package]\nname = \"conformance-rows-probe\"\nversion = \"0.0.0\"\nedition = \"2024\"\nautotests = false\n\n[dependencies]\n")
	var names []string
	for d := range deps {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		fmt.Fprintf(&toml, "%s = { path = %q }\n", d, filepath.Join(genDir, d))
	}
	for _, row := range rows {
		fmt.Fprintf(&toml, "\n[[test]]\nname = %q\npath = %q\n", row, c.rowPath(row, ".rs"))
	}
	if err := os.WriteFile(filepath.Join(probe, "Cargo.toml"), []byte(toml.String()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(probe, "src", "lib.rs"), nil, 0o644); err != nil {
		return err
	}
	line := cargo + " build --tests --offline --keep-going --message-format=json --manifest-path " + strconv.Quote(filepath.Join(probe, "Cargo.toml"))
	cmd := exec.Command("sh", "-c", line)
	cmd.Dir = c.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	buildErr := cmd.Run()
	diags := map[string]*strings.Builder{}
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var msg struct {
			Reason string `json:"reason"`
			Target struct {
				Name string   `json:"name"`
				Kind []string `json:"kind"`
			} `json:"target"`
			Executable *string `json:"executable"`
			Message    struct {
				Level    string `json:"level"`
				Rendered string `json:"rendered"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		isTest := len(msg.Target.Kind) == 1 && msg.Target.Kind[0] == "test"
		switch {
		case msg.Reason == "compiler-artifact" && isTest && msg.Executable != nil:
			c.exe[msg.Target.Name] = *msg.Executable
		case msg.Reason == "compiler-message" && isTest && msg.Message.Level == "error":
			b := diags[msg.Target.Name]
			if b == nil {
				b = &strings.Builder{}
				diags[msg.Target.Name] = b
			}
			b.WriteString(msg.Message.Rendered)
		}
	}
	for _, row := range rows {
		if _, ok := c.exe[row]; ok {
			continue
		}
		why := "the cargo build produced no test executable for it"
		if b := diags[row]; b != nil {
			why = tail(b.String(), 30)
		} else if buildErr != nil {
			why = fmt.Sprintf("cargo: %v\n%s", buildErr, tail(stderr.String(), 30))
		}
		c.pre[row] = fmt.Errorf("does not build:\n%s", why)
	}
	return nil
}

var rustPathRe = regexp.MustCompile(`(?m)^\s*#\[path\s*=\s*"([^"]+)"\]`)

// rustPathUnits generates what a row's #[path] names under
// build/tables-generated-rust when the tree's crates do not hold it. A row
// may include one schema's generated files flat, as a local module
// (#[path = ".../build/tables-generated-rust/v1/fixed_runtime.rs"], or
// .../vnew_cfloat_res_refine/build_version.rs), from a directory that is
// not one of the tree's crates (those keep their files under src/). The
// directory's schema is test/tables/<D>.schema with the name's first word
// upper-cased (v1 is V1.schema, vnew_cfloat_res_refine is
// VNEW_cfloat_res_refine.schema), generated flat into that directory once
// per run. A path with no such schema stays missing, and the row's build
// says so by name.
func (c *ctx) rustPathUnits(rows []string, genDir string, crates map[string]bool) error {
	schema, err := env("SCHEMA")
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, row := range rows {
		src := c.rowPath(row, ".rs")
		text, rerr := os.ReadFile(src)
		if rerr != nil {
			return rerr
		}
		for _, m := range rustPathRe.FindAllStringSubmatch(string(text), -1) {
			p := filepath.Clean(filepath.Join(filepath.Dir(src), m[1]))
			rel, rerr := filepath.Rel(genDir, p)
			if rerr != nil || strings.HasPrefix(rel, "..") || !strings.Contains(rel, string(filepath.Separator)) {
				continue
			}
			dir, _, _ := strings.Cut(rel, string(filepath.Separator))
			if crates[dir] || done[dir] {
				continue
			}
			if _, serr := os.Stat(p); serr == nil {
				continue
			}
			done[dir] = true
			word, rest, _ := strings.Cut(dir, "_")
			name := strings.ToUpper(word)
			if rest != "" {
				name += "_" + rest
			}
			schemaPath := filepath.Join(c.root, "test", "tables", name+".schema")
			if _, serr := os.Stat(schemaPath); serr != nil {
				continue
			}
			out := filepath.Join(genDir, dir)
			if err := os.RemoveAll(out); err != nil {
				return err
			}
			cmd := exec.Command(schema, "generate", "--lang", "rust", "--out", out, schemaPath)
			cmd.Dir = c.root
			if b, gerr := cmd.CombinedOutput(); gerr != nil {
				return fmt.Errorf("generating %s for the rows' #[path] includes: %w\n%s", out, gerr, tail(string(b), 20))
			}
		}
	}
	return nil
}

var rustPassedRe = regexp.MustCompile(`test result: ok\. (\d+) passed`)

func rowRust(c *ctx, row string) error {
	c.resetLog(row)
	exe := c.exe[row]
	// One test thread: the tests of a row run in name order, so the first
	// failure, and with it the cause the ledger states, is the same on every
	// run (with threads, W13's two failing tests raced for first). The rows
	// themselves still run in parallel.
	cmd := exec.Command(exe, "--test-threads=1")
	cmd.Dir = c.root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if werr := os.WriteFile(filepath.Join(c.work, row+".log"), out.Bytes(), 0o644); werr != nil && err == nil {
		err = werr
	}
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", exe, err, tail(out.String(), 30))
	}
	m := rustPassedRe.FindStringSubmatch(out.String())
	if m == nil || m[1] == "0" {
		return fmt.Errorf("the row's test binary ran no test (a 0-test pass is not a pass)\n%s", tail(out.String(), 10))
	}
	return nil
}
