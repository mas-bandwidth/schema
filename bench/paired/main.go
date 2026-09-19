// paired measures the existing packet and table runners over identical logical
// records. It deliberately preserves separate generated packet storage, corpora
// and CSV identities; only this verified pairing may divide the two families.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// languages IS THE PUBLISHED SET, and it is what a confirmation pass and a
// rendered report are sealed over (provenance.go's passInputNames). Each one
// measures BOTH wires over the same 64 logical records, which is the only
// thing that licenses the division, so the confirmation pass, its window and
// its board are defined over exactly these. A leg is not published until every
// round of a seven-round pass carries it, so adding a name here changes what an
// old pass renders as — which is why a new leg arrives in one of the two lists
// below and moves here when it is promoted.
var languages = []string{"cpp", "c", "go", "cs"}

// AN UNPUBLISHED PAIRED LANGUAGE has BOTH wires, so it has a ratio of its own,
// but no published pass carries it yet. `-mode fast` is the DIAGNOSTIC mode and
// takes one of these ALONE; `-mode run` still takes the published four and
// nothing else.
var unpublishedLanguages = []string{"elixir"}

// A TABLE-ONLY LANGUAGE measures the table wire and NEVER a ratio.
//
// rust is here because its packet leg does not meet this driver's contract
// rather than because somebody chose to skip it. bench/rust/src/main.rs is a
// real, maintained packet runner — its CSV is already the seventeen columns —
// and it still cannot be invoked here: it has neither `--gate` nor
// `--iterations`, the two flags this driver passes on every invocation
// (`--iterations` is how one uniform count is held across every language and
// round, §2.1, and `--gate` is the no-clock correctness pass), it reports the
// median of seven runs of its own choosing where this driver requires one
// measured run per round and aggregates across rounds itself, and it measures
// the packet corpus's own variant set rather than this pairing's. Those are
// three changes to a leg whose numbers are already published, so they are
// separate work with their own ruling; filling this driver's second wire with
// a fabricated row would be inventing a measurement. So rust rides the table
// wire alone and appears in no ratio, no confirmation pass and no board.
//
// java is here for the same reason arrived at down a different road:
// bench/java/Main.java is the TYPE BOARD's runner, with neither `--gate` nor
// `--iterations` either, and the paired set above is the four the board is
// defined over. The table leg (bench/tables/java/TableMain.java) is this
// driver's own shape. A packet leg here is separate work with its own ruling,
// so java too rides the table wire alone and appears in no ratio, no
// confirmation pass and no board; its packet number is the type board's,
// measured by its own runner over the same sixty-four records.
//
// js is here because its packet leg does not exist to be run rather than
// because somebody chose to skip it: bench/js/main.mjs imports the serialize.js
// sibling runtime, has neither `--gate` nor `--iterations` — the two flags this
// driver passes on every invocation — and appends the §5.1 `codec` column, so
// its rows are eighteen columns where parseRows requires seventeen. The table
// codec has none of those problems: the generated JS table modules import no
// runtime at all. A packet leg is separate work with its own ruling, and
// filling this driver's second wire with a fabricated row would be inventing a
// measurement, so js rides the table wire alone and appears in no ratio, no
// confirmation pass and no board.
//
// dart is here for the same reason js is. bench/dart/main.dart is the TYPE
// BOARD's packet runner and has neither `--gate` nor `--iterations` — the two
// flags this driver passes on every invocation — and it reports a median of
// seven runs of its own choosing where this driver requires one measured run
// per round. The table leg (bench/tables/dart/table_main.dart) is this driver's
// own shape, built AOT by `dart compile exe` so the generated libraries ride in
// the same binary as the runner. A packet leg here is separate work with its
// own ruling, and fabricating a packet row to fill the driver's second wire
// would be inventing a measurement, so dart too rides the table wire alone and
// appears in no ratio, no confirmation pass and no board.
var tableOnlyLanguages = []string{"rust", "java", "js", "dart"}

// A PENDING LANGUAGE is a row the published table names that this driver has
// no leg for at all: no generator invocation, no build step, no runner, no
// wire. It is deliberately NOT in allLanguages(), so `-langs` refuses it and
// nothing can generate, build, gate or measure it. The nine-language table
// carries the name so the row it will one day fill reads as absent rather than
// silently missing, and `bench/paired/table.go` takes it from here rather than
// keeping a list of its own.
//
// IT IS EMPTY, AND THE MACHINERY STAYS. dart was the last name here and its
// table leg has landed (bench/tables/dart/table_main.dart), so every row the
// nine-language table names is now a leg this driver can generate, build, gate
// and measure. The list, `absentReason`'s "no leg in this driver yet" branch
// and table.go's derivation of the row set from this roster all remain, because
// the next language's runner will land before its wiring does exactly as this
// one's did, and the honest answer for it then is the one they give.
var pendingLanguages = []string{}

// names is the ONE display-name map: every language this driver knows, plus
// the pending rows, so nothing downstream keeps a second one.
var names = map[string]string{"c": "C", "cpp": "C++", "go": "Go", "cs": "C#", "elixir": "Elixir", "rust": "Rust", "java": "Java", "js": "JavaScript", "dart": "Dart"}

// allLanguages IS EVERY NAME -langs ACCEPTS: the published four, the paired
// legs that are measurable but not published yet, and the table-only legs.
func allLanguages() []string {
	out := append([]string{}, languages...)
	out = append(out, unpublishedLanguages...)
	return append(out, tableOnlyLanguages...)
}

// wiresFor names the wires a language actually has a runner for. Every loop
// that walks a language's legs walks this and not the literal pair, so a
// table-only language is never asked for a packet row it cannot produce.
func wiresFor(lang string) []string {
	if contains(tableOnlyLanguages, lang) {
		return []string{"table"}
	}
	return []string{"packet", "table"}
}

// checkRequestShape holds the rule that A TABLE-ONLY LANGUAGE IS NEVER IN A
// MIXED REQUEST, and names the one exception: `build`.
//
// The exception is the build MANIFEST, which is one file. generateAndBuild does
// per-language work and compares nothing — it walks `wiresFor(lang)`, which
// already knows a table-only leg has no packet wire — and then writes the whole
// of build/paired/build.json, replacing what was there. fastMeasure refuses to
// measure a leg that manifest does not carry ("cached build lacks %s/%s"), so
// every leg of one sitting has to be built by ONE invocation, and the
// nine-language sitting is paired legs and table-only legs together. Splitting
// that build in two would leave a manifest describing half of it.
//
// GATE KEEPS THE RULE. It is per-language work too, but nothing forces it into
// one invocation, so bench/paired/nine.sh gates one language at a time — every
// single-language request is one shape or the other — and this driver never has
// to decide what a half-paired gate would mean. Every other mode reads the
// request as a set to compare and takes one shape or the other, whole.
func checkRequestShape(mode string, langs []string) error {
	if mode == "build" || onlyTableLanguages(langs) {
		return nil
	}
	for _, lang := range langs {
		if contains(tableOnlyLanguages, lang) {
			return errors.New("a table-only language is requested alone: -langs " + lang)
		}
	}
	return nil
}

// onlyTableLanguages reports whether every requested language is table-only.
// A request is one shape or the other and never a mixture: a diagnostic that
// paired some languages and not others would print a ratio table with holes.
func onlyTableLanguages(langs []string) bool {
	for _, lang := range langs {
		if !contains(tableOnlyLanguages, lang) {
			return false
		}
	}
	return len(langs) > 0
}

// twinTolerance is the C vs C++ paired-row band (percent). Rows within it
// do not fail the twin check. 0 disables. Flag --twin-tolerance.
var twinTolerance = 10.0

// unpublishedAlone reports the DIAGNOSTIC shape: one leg that no published
// pass carries, asked for on its own. It seals nothing and enters no board.
func unpublishedAlone(langs []string) bool {
	return len(langs) == 1 && !contains(languages, langs[0])
}

// THE BEAM TOOLCHAIN, pinned exactly as make/elixir.mk pins it: the repo-local
// unpacked dist/ by default, and whatever is on PATH when BEAM_PATH names a
// directory that is not there (which is what CI has). The `elixir` launcher
// finds `erl` through PATH, so both bin directories ride together.
func beamPath() string {
	return setting("BEAM_PATH", abs("dist/otp-29.0.5/bin")+string(os.PathListSeparator)+abs("dist/elixir-1.20.4/bin"))
}

// elixirLeg is the leg's entry point per wire: the packet runner is the same
// one bench/run.sh drives, and the table runner is the fixed form's own.
func elixirLeg(wire string) string {
	if wire == "packet" {
		return "bench/elixir/main.exs"
	}
	return "bench/tables/elixir/main.exs"
}

// elixirInputs is what a run of an Elixir leg IS, for provenance: there is no
// compiled artifact to hash, so the identity of the leg is its own sources and
// the generated modules it loads. Every one of them is recorded in build.json
// and re-hashed by readBuild, so an edit after the build is refused exactly as
// a recompiled binary would be.
func elixirInputs(wire string) ([]string, error) {
	out := []string{elixirLeg(wire), filepath.Join(filepath.Dir(elixirLeg(wire)), "runner.exs")}
	gen := "generated/bench/elixir"
	if wire != "packet" {
		gen = "generated/bench/paired/elixir"
	}
	found, err := filepath.Glob(filepath.Join(gen, "*.ex"))
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no generated Elixir under %s", gen)
	}
	sort.Strings(found)
	return append(out, found...), nil
}

// elixirManifest writes the one path binary() can name for an interpreted leg:
// a sorted list of every input and its SHA-256. It is a convenience for a
// reader of build/paired — the inputs themselves are recorded individually and
// are what actually holds the leg to its build.
func elixirManifest(wire string) (string, error) {
	inputs, err := elixirInputs(wire)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range inputs {
		h, err := hashFile(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s  %s\n", h, filepath.ToSlash(p))
	}
	path := binary(wire, "elixir")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(b.String()), 0644)
}

const header = "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline"

type buildInfo struct {
	Date        string            `json:"date"`
	Host        string            `json:"host"`
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	CPU         string            `json:"cpu"`
	OSVersion   string            `json:"os_version"`
	Revision    string            `json:"revision"`
	Dirty       bool              `json:"dirty"`
	Tools       map[string]string `json:"tools"`
	Runtimes    map[string]string `json:"runtimes"`
	Binaries    map[string]string `json:"binary_sha256"`
	Corpora     map[string]string `json:"corpus_sha256"`
	CorpusIDs   map[string]string `json:"corpus_ids"`
	Environment map[string]string `json:"environment"`
	Rounds      int               `json:"rounds,omitempty"`
}

// Only settings that affect the measured runtimes belong in provenance.
// Never dump the process environment (it can contain credentials).
var measuredEnvironment = []string{"DOTNET_TieredCompilation", "DOTNET_TieredPGO", "DOTNET_TC_QuickJitForLoops", "DOTNET_ReadyToRun", "DOTNET_gcServer", "COMPlus_TieredCompilation", "COMPlus_TieredPGO", "COMPlus_ReadyToRun", "COMPlus_gcServer", "GOMAXPROCS", "GOGC", "GOMEMLIMIT", "GOFLAGS", "GOAMD64", "GOARM64", "CGO_ENABLED"}

func cpuName() string {
	if runtime.GOOS == "darwin" {
		b, _ := capture(command{args: []string{"sysctl", "-n", "machdep.cpu.brand_string"}})
		return strings.TrimSpace(string(b))
	}
	b, _ := os.ReadFile("/proc/cpuinfo")
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(line, "model name") {
			_, name, _ := strings.Cut(line, ":")
			return strings.TrimSpace(name)
		}
	}
	return "unavailable"
}

type command struct {
	args []string
	dir  string
	// env is added to this process's environment, never replacing it: an
	// interpreted leg needs its pinned toolchain on PATH, and the Rust leg's
	// optimization level is a cargo setting rather than a compiler flag on a
	// command line. Nothing here drops what the operator already exported, and
	// nothing else about the measured environment may move (see
	// measuredEnvironment).
	env []string
}

func (c command) environment() []string {
	if len(c.env) == 0 {
		return nil
	}
	return append(os.Environ(), c.env...)
}

// program is argv[0], resolved against the PATH this command CARRIES rather
// than the driver's own. exec.Command looks a bare name up in the parent's
// PATH before cmd.Env is ever consulted, so a pinned toolchain reached only
// through c.env would not be found at all — the leg would fail with "not in
// $PATH" while sitting in the directory it was pinned to.
func (c command) program() string {
	name := c.args[0]
	if len(c.env) == 0 || strings.ContainsRune(name, filepath.Separator) {
		return name
	}
	for _, kv := range c.env {
		value, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		for dir := range strings.SplitSeq(value, string(os.PathListSeparator)) {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				return candidate
			}
		}
	}
	return name
}

func execute(c command) error {
	fmt.Fprintln(os.Stderr, "+", strings.Join(append(append([]string{}, c.env...), c.args...), " "))
	cmd := exec.Command(c.program(), c.args[1:]...)
	cmd.Dir = c.dir
	cmd.Env = c.environment()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func capture(c command) ([]byte, error) {
	cmd := exec.Command(c.program(), c.args[1:]...)
	cmd.Dir = c.dir
	cmd.Env = c.environment()
	cmd.Stderr = os.Stderr
	return cmd.Output()
}
func run(args ...string) error { return execute(command{args: args}) }
func setting(name, fallback string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	return fallback
}
func abs(p string) string {
	s, e := filepath.Abs(p)
	if e != nil {
		panic(e)
	}
	return s
}
func binary(wire, lang string) string {
	// AN INTERPRETED LEG HAS NO BUILD PRODUCT, so the thing this driver pins,
	// hashes and runs is the runner source itself. Its generated modules are
	// hashed beside it (generateAndBuild), so the recorded identity covers the
	// whole unit that runs and not only the entry point.
	if lang == "js" {
		return filepath.Join("bench", "tables", "js", "table_main.mjs")
	}
	// THE JAVA LEG'S BUILD PRODUCT is a directory of classfiles, so the thing
	// this driver pins and hashes is the runner's own class in it; the
	// generated sources it was compiled beside are hashed too
	// (generateAndBuild), so the recorded identity covers the whole unit that
	// runs and not only the entry point.
	if lang == "java" {
		return filepath.Join(javaClassDir, "TableMain.class")
	}
	p := filepath.Join("build", "paired", wire+"-"+lang)
	if lang == "elixir" {
		// AN INTERPRETED LEG HAS NO EXECUTABLE. What stands in its place is the
		// manifest of its inputs and their hashes; the inputs are recorded and
		// verified individually beside it (elixirInputs).
		return filepath.Join(p, "leg.sha256")
	}
	if lang == "cs" {
		if wire == "packet" {
			return filepath.Join(p, "schemabench.dll")
		}
		return filepath.Join(p, "paired.dll")
	}
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	return p
}

// cargoExecutable names the cargo the Rust leg builds with. make/rust.mk finds
// it in the rustup keg, which is not on PATH by default, and PREFERS it —
// prepending RUSTUP_BIN — so the paired leg is built by the same cargo the rest
// of the Rust leg's gates use rather than by whatever a shell happens to
// expose. build.json records its version either way.
// The cargo target directory: under build/, which is not tracked, so a paired
// build never leaves artefacts beside a runner's source.
const rustTargetDir = "build/paired/rust"

// The Rust unit's manifest is tracked and hand-written (generated/bench/paired/
// rust/Cargo.toml says why), and it names its serialize.rs dependency through
// ONE SYMLINK UNDER build/ rather than through a checkout path. pointRustManifest
// requires the manifest and points that symlink at SERIALIZE_RS — the sibling
// `../serialize.rs` by default, which is the keeper's layout and resolves to the
// very same crate the path used to name directly.
//
// NOTHING TRACKED IS WRITTEN. The old version rewrote the dependency line in
// place, which dirtied a tracked file on every checkout that is not beside
// serialize.rs — and `-mode fast`, `-mode table` and bench/paired/nine.sh all
// refuse a dirty checkpoint, so the sitting could not start anywhere but the
// keeper's own tree. `dirty` in build.json now means the operator changed
// something, which is the only thing it was ever meant to mean.
func pointRustManifest() error {
	const manifest = "generated/bench/paired/rust/Cargo.toml"
	if _, e := os.Stat(manifest); e != nil {
		return fmt.Errorf("the paired rust unit's manifest is missing (%s); it is tracked build wiring, not generator output: %w", manifest, e)
	}
	target := abs(setting("SERIALIZE_RS", "../serialize.rs"))
	if _, e := os.Stat(filepath.Join(target, "Cargo.toml")); e != nil {
		return fmt.Errorf("the paired rust leg needs a serialize.rs checkout at %s (set SERIALIZE_RS): %w", target, e)
	}
	if e := os.MkdirAll(filepath.Join("build", "paired"), 0755); e != nil {
		return e
	}
	// Replace the link rather than write through it: os.Symlink refuses an
	// existing name, and a stale link from an earlier SERIALIZE_RS would
	// otherwise decide this build.
	link := filepath.Join("build", "paired", "serialize.rs")
	if e := os.Remove(link); e != nil && !os.IsNotExist(e) {
		return e
	}
	return os.Symlink(target, link)
}
func cargoExecutable() string { return rustupTool("CARGO", "cargo") }

// rustcExecutable names the compiler behind that cargo. It is recorded for the
// same reason `cc --version` is: the codegen is the compiler's, not the build
// tool's.
func rustcExecutable() string { return rustupTool("RUSTC", "rustc") }

func rustupTool(env, name string) string {
	if s := os.Getenv(env); s != "" {
		return s
	}
	keg := filepath.Join(setting("RUSTUP_BIN", "/opt/homebrew/opt/rustup/bin"), name)
	if _, e := os.Stat(keg); e == nil {
		return keg
	}
	return name
}

// generatedModules names the generated sources a leg COMPILES rather than
// links (java) or LOADS (js), so readBuild verifies them the way it verifies a
// compiled leg's binary. It is a GLOB and not a list: the emitter decides how
// many modules a unit has, and a list here would be one more place to keep in
// step with it — and one more place naming a corpus type, which this driver
// has no business doing. rust's unit is inside its binary, and the binary's
// own hash already covers it.
func generatedModules(lang string) ([]string, error) {
	if lang != "java" && lang != "js" {
		return nil, nil
	}
	found, err := filepath.Glob(filepath.Join("generated", "bench", "paired", lang, "*."+lang))
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no generated %s modules for the paired unit", lang)
	}
	sort.Strings(found)
	return found, nil
}

// javaClassDir is where the Java table leg's classfiles land.
const javaClassDir = "build/paired/table-java"

// javaExecutable and javacExecutable are the JDK's two halves, the same pins
// make/java.mk carries: the repository-local JDK when it is there, and the one
// on PATH otherwise, with JAVA and JAVAC overriding both.
func javaExecutable() string  { return jdkTool("JAVA", "java") }
func javacExecutable() string { return jdkTool("JAVAC", "javac") }
func jdkTool(name, tool string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	local := filepath.Join("dist", "jdk-21.0.12.1", "Contents", "Home", "bin", tool)
	if _, err := os.Stat(local); err == nil {
		return local
	}
	return tool
}
func nodeExecutable() string { return setting("NODE", "node") }

// dartExecutable names the Dart SDK this leg builds with, pinned exactly as
// make/dart.mk pins it: the repository-local unpacked SDK when it is there, and
// the one on PATH otherwise (which is what CI has), with DART overriding both.
// build.json records its version either way.
func dartExecutable() string {
	if s := os.Getenv("DART"); s != "" {
		return s
	}
	local := filepath.Join("dist", "dart-sdk-3.13.2", "bin", "dart")
	if _, err := os.Stat(local); err == nil {
		return abs(local)
	}
	return "dart"
}
func runner(wire, lang string, args ...string) command {
	a := []string{binary(wire, lang)}
	var env []string
	switch lang {
	case "cs":
		a = append([]string{"dotnet"}, a...)
	case "js":
		a = append([]string{nodeExecutable()}, a...)
	case "java":
		// The class, by name, on the classpath the build filled.
		a = []string{javaExecutable(), "-cp", javaClassDir, "TableMain"}
	case "elixir":
		// The leg is a script, and it is spawned from the REPOSITORY ROOT like
		// every other leg, so its corpus paths arrive through --wire-dir and
		// --variant-dir rather than through a working directory.
		a = []string{"elixir", elixirLeg(wire)}
		env = []string{"PATH=" + beamPath() + string(os.PathListSeparator) + os.Getenv("PATH")}
	}
	if wire == "table" {
		a = append(a, "--indexed", "--wire-dir", "bench/paired/corpus", "--variant-dir", "bench/paired/corpus")
	} else {
		a = append(a, "--wire-dir", "testdata/wire", "--variant-dir", "bench/corpus/variants")
	}
	return command{args: append(a, args...), env: env}
}
func hashFile(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func gitValue(dir string, args ...string) string {
	b, e := capture(command{args: append([]string{"git", "-C", dir}, args...)})
	if e != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(b))
}
func corpusID(wire string) (string, error) {
	paths := map[string]string{}
	if wire == "packet" {
		paths["bench_mixed.bin"] = "testdata/wire/bench_mixed.bin"
		paths["bench_mixed.variants.bin"] = "bench/corpus/variants/bench_mixed.variants.bin"
	} else {
		// bench_fixed.bin and bench_fixed.layout are the FIXED FORM's half of
		// the same 64 logical records (docs/SPEC-TABLES.md §3.4). They ride the
		// table corpus id because they are the same corpus: a row measured
		// against one of these files is not divisible against a row measured
		// before they existed.
		for _, name := range []string{"bench_table.bin", "bench_table.lengths", "bench_table.variants.bin", "bench_fixed.bin", "bench_fixed.layout"} {
			paths[name] = "bench/paired/corpus/" + name
		}
	}
	keys := []string{}
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := uint64(0xcbf29ce484222325)
	feed := func(b []byte) {
		for _, v := range b {
			h ^= uint64(v)
			h *= 0x100000001b3
		}
	}
	for _, k := range keys {
		b, e := os.ReadFile(paths[k])
		if e != nil {
			return "", e
		}
		feed([]byte(k))
		feed([]byte{0})
		feed(b)
	}
	return fmt.Sprintf("%016x", h), nil
}
func generateAndBuild(langs []string) error {
	if e := os.MkdirAll("build/paired", 0755); e != nil {
		return e
	}
	schema := "build/paired/schema"
	if runtime.GOOS == "windows" {
		schema += ".exe"
	}
	if e := run("go", "build", "-o", schema, "./cmd/schema"); e != nil {
		return e
	}
	for _, lang := range langs {
		paired := []string{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"}
		if lang == "elixir" {
			// THE UNIT'S SCHEMA FILES ARE COPIED UNDER OTHER BASENAMES, and only
			// that: a declaration whose name is its own file's basename collides
			// with the module the Elixir backend writes for that file
			// (docs/SPEC-TABLES.md §11), which is the checker working rather
			// than a problem. No declaration moves, so no id and no layout byte
			// moves either. make/elixir.mk's tables-elixir-fixed-bench makes the
			// same copy for the same reason.
			src := "build/paired/elixir-src"
			if e := os.MkdirAll(src, 0755); e != nil {
				return e
			}
			renamed := []string{"Bench.schema", "Wrap.schema"}
			for i, to := range renamed {
				b, e := os.ReadFile(paired[i])
				if e != nil {
					return e
				}
				to = filepath.Join(src, to)
				if e := os.WriteFile(to, b, 0644); e != nil {
					return e
				}
				renamed[i] = to
			}
			paired = renamed
		}
		if e := run(append([]string{schema, "generate", "--lang", lang, "--out", "generated/bench/paired/" + lang}, paired...)...); e != nil {
			return e
		}
		// The standalone packet unit is the packet leg's, so a table-only
		// language does not generate it: nothing here would compile it and
		// emitting it would imply a leg that does not exist.
		if contains(tableOnlyLanguages, lang) {
			continue
		}
		if e := run(schema, "generate", "--lang", lang, "--out", "generated/bench/"+lang, "bench/corpus/Bench.schema"); e != nil {
			return e
		}
	}
	cxx, cc := setting("CXX", "c++"), setting("CC", "cc")
	ser, serC, serGo, serCs := abs(setting("SERIALIZE", "../serialize")), abs(setting("SERIALIZE_C", "../serialize.c")), abs(setting("SERIALIZE_GO", "../serialize.go")), abs(setting("SERIALIZE_CS", "../serialize.cs"))
	opt := setting("BENCH_OPT_LEVEL", "O3")
	if opt != "O2" && opt != "O3" {
		return errors.New("BENCH_OPT_LEVEL must be O2 or O3")
	}
	stamp := "-DBENCH_OPT=\"" + opt + "\""
	cppFlags := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-ffp-contract=off", "-fno-rtti", "-" + opt, "-DNDEBUG", stamp}
	if version, e := capture(command{args: []string{cxx, "--version"}}); e == nil && (strings.Contains(strings.ToLower(string(version)), "gcc") || strings.Contains(strings.ToLower(string(version)), "g++")) {
		cppFlags = append(cppFlags, "-Wno-class-memaccess", "-Wno-type-limits")
	}
	cFlags := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-" + opt, "-DNDEBUG", stamp}
	if version, e := capture(command{args: []string{cc, "--version"}}); e == nil && strings.Contains(strings.ToLower(string(version)), "gcc") {
		cFlags = append(cFlags, "-Wno-stringop-truncation")
	}
	// The independent C++ bridge is the equivalence oracle even for a subset pass.
	if !contains(langs, "cpp") {
		if e := run(schema, "generate", "--lang", "cpp", "--out", "generated/bench/paired/cpp", "bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"); e != nil {
			return e
		}
	}
	args := append([]string{cxx}, cppFlags...)
	args = append(args, "-Igenerated/bench/paired/cpp", "-I"+ser, "test/bench/paired_main.cpp", "-o", "build/paired/corpus")
	if e := run(args...); e != nil {
		return e
	}
	if e := run("build/paired/corpus", "verify"); e != nil {
		return e
	}
	for _, lang := range langs {
		switch lang {
		case "cpp":
			a := append([]string{cxx}, cppFlags...)
			a = append(a, "-DBENCH_MATCHED", "-Igenerated/bench/paired/cpp", "-I"+ser, "bench/tables/cpp/table_main.cpp", "-o", binary("table", lang))
			if e := run(a...); e != nil {
				return e
			}
			a = append([]string{cxx}, cppFlags...)
			a = append(a, "-DSERIALIZE_RELEASE", "-Igenerated/bench/cpp", "-I"+ser, "bench/cpp/bench_main.cpp", "-o", binary("packet", lang))
			if e := run(a...); e != nil {
				return e
			}
		case "c":
			a := append([]string{cc}, cFlags...)
			// serialize.c is a translation unit, not a header: the table leg links it
			// exactly as the packet leg below does. Its form-1 arm calls into the
			// library (_serialize_uint128_make, the generated save/load bodies), and
			// the bench measures BOTH forms, so the objects are not optional.
			a = append(a, "-ffp-contract=off", "-DBENCH_MATCHED", "-Igenerated/bench/paired/c", "-I"+serC, "bench/tables/c/table_main.c", filepath.Join(serC, "serialize.c"), "-o", binary("table", lang), "-lm")
			if e := run(a...); e != nil {
				return e
			}
			a = append([]string{cc}, cFlags...)
			a = append(a, "-Igenerated/bench/c", "-I"+serC, "bench/c/bench_main.c", filepath.Join(serC, "serialize.c"), "-o", binary("packet", lang), "-lm")
			if e := run(a...); e != nil {
				return e
			}
		case "go":
			if e := os.WriteFile("generated/bench/paired/go/go.mod", []byte("module benchtable\n\ngo 1.24\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../../../../serialize.go\n"), 0644); e != nil {
				return e
			}
			for _, wire := range []string{"table", "packet"} {
				dir := "bench/go"
				dep := "bench"
				target := abs("generated/bench/go")
				if wire == "table" {
					dir = "bench/paired/go"
					dep = "benchtable"
					target = abs("generated/bench/paired/go")
				}
				mod := abs("build/paired/" + wire + ".mod")
				b, e := os.ReadFile(filepath.Join(dir, "go.mod"))
				if e != nil {
					return e
				}
				if e = os.WriteFile(mod, b, 0644); e != nil {
					return e
				}
				if e := execute(command{args: []string{"go", "mod", "edit", "-modfile", mod, "-replace", dep + "=" + target, "-replace", "github.com/mas-bandwidth/serialize.go=" + serGo}, dir: dir}); e != nil {
					return e
				}
				a := []string{"go", "build", "-modfile", mod, "-o", abs(binary(wire, lang))}
				if wire == "table" {
					a = append(a, "-tags", "matched", "../../tables/go/table_main.go", "../../tables/go/shape_matched.go")
				} else {
					a = append(a, ".")
				}
				if e := execute(command{args: a, dir: dir}); e != nil {
					return e
				}
			}
		case "cs":
			for _, wire := range []string{"table", "packet"} {
				project := "bench/cs"
				if wire == "table" {
					project = "bench/paired/cs"
				}
				if e := run("dotnet", "build", project, "-c", "Release", "-v", "q", "--nologo", "-property:UseSharedCompilation=false", "-property:SerializeCsRoot="+serCs, "--output", filepath.Dir(binary(wire, lang))); e != nil {
					return e
				}
			}
		case "elixir":
			// NOTHING IS LINKED AND NOTHING IS CACHED: the leg is a script that
			// compiles the generated modules into the VM it measures in. What
			// this stage does instead is (a) hold the generated Elixir to the
			// SAME TWO GATES the make targets hold it to — `mix format
			// --check-formatted`, because the emitter emits the formatter's own
			// shape rather than being checked afterwards, and `elixirc
			// --warnings-as-errors`, because a warning in generated code is a
			// defect in the emitter — and (b) write the manifest that stands in
			// for an executable in the provenance.
			env := []string{"PATH=" + beamPath() + string(os.PathListSeparator) + os.Getenv("PATH")}
			for _, wire := range []string{"table", "packet"} {
				inputs, e := elixirInputs(wire)
				if e != nil {
					return e
				}
				sources := inputs[2:] // the generated modules; [0] and [1] are the leg
				if e := execute(command{args: append([]string{"mix", "format", "--check-formatted"}, inputs...), env: env}); e != nil {
					return e
				}
				ebin := filepath.Join("build", "paired", wire+"-elixir", "ebin")
				if e := os.MkdirAll(ebin, 0755); e != nil {
					return e
				}
				if e := execute(command{args: append([]string{"elixirc", "--warnings-as-errors", "-o", ebin}, sources...), env: env}); e != nil {
					return e
				}
				if _, e := elixirManifest(wire); e != nil {
					return e
				}
			}
		case "rust":
			// THE MANIFEST IS BUILD WIRING AND IS TRACKED (the Rust emitter
			// writes only .rs files; make/rust.mk says the same of every other
			// Rust unit's Cargo.toml), so this step does not write it — it
			// requires it, and points the ONE thing an operator can move: the
			// build/ symlink the manifest names for serialize.rs. Nothing under
			// generated/ is touched, on any checkout.
			if e := pointRustManifest(); e != nil {
				return e
			}
			// The optimization level is a CARGO SETTING, not a flag on a
			// command line, so it is set on the build and stamped into the
			// binary through the same option_env! seam the packet Rust leg
			// opened: a row's `opt` column then says what the build actually
			// was instead of repeating a constant nobody checked.
			cargoEnv := []string{"BENCH_OPT=" + opt, "CARGO_PROFILE_RELEASE_OPT_LEVEL=" + strings.TrimPrefix(opt, "O")}
			if e := execute(command{args: []string{cargoExecutable(), "build", "--release", "--quiet", "--manifest-path", "bench/tables/rust/Cargo.toml", "--target-dir", rustTargetDir}, env: cargoEnv}); e != nil {
				return e
			}
			// Replace the inode rather than writing over it: a copy onto a
			// binary another process is running corrupts it in place, and a
			// long sitting runs this one (make/rust.mk's conformance target
			// carries the same note for the same reason).
			built, e := os.ReadFile(filepath.Join(rustTargetDir, "release", "table-rust"))
			if e != nil {
				return e
			}
			if e = os.Remove(binary("table", lang)); e != nil && !os.IsNotExist(e) {
				return e
			}
			if e = os.WriteFile(binary("table", lang), built, 0755); e != nil {
				return e
			}
		case "java":
			// The generated unit and the runner, compiled beside each other
			// into one classpath under the same javac flags the Java legs
			// use everywhere (make/java.mk): -Werror, because the generated
			// sources are compiled by the consumer's javac. Then the gate.
			if e := os.RemoveAll(javaClassDir); e != nil {
				return e
			}
			if e := os.MkdirAll(javaClassDir, 0755); e != nil {
				return e
			}
			modules, e := generatedModules(lang)
			if e != nil {
				return e
			}
			javac := []string{"--release", "17", "-Xlint:all", "-Werror", "-d", javaClassDir}
			javac = append(javac, modules...)
			javac = append(javac, filepath.Join("bench", "tables", "java", "TableMain.java"))
			if e := run(append([]string{javacExecutable()}, javac...)...); e != nil {
				return fmt.Errorf("javac is required for the java leg (set JAVAC): %w", e)
			}
			if e := execute(runner("table", lang, "--gate")); e != nil {
				return e
			}
		case "js":
			// Nothing compiles. The build step's whole job is to prove the
			// interpreter is here and that the leg loads and gates, which is
			// the same evidence a compile gives the other legs: a leg that
			// cannot start is a build failure and not a skipped row.
			if e := run(nodeExecutable(), "--version"); e != nil {
				return fmt.Errorf("node is required for the js leg (set NODE): %w", e)
			}
			if e := execute(runner("table", lang, "--gate")); e != nil {
				return e
			}
		case "dart":
			// THE TIMED FORM IS THE AOT EXECUTABLE, the same one
			// bench/dart/main.dart's packet rows are measured from: `dart
			// compile exe` compiles the generated libraries into one binary
			// with the runner, so this leg HAS a build product to hash and
			// needs none of the interpreted legs' manifest machinery, and its
			// `linkage` column says `aot` because that is what the binary is.
			//
			// The two gates the generated Dart answers everywhere else
			// (make/dart.mk) run first and on the generated unit: `dart
			// analyze` over the WHOLE unit, because a diagnostic in generated
			// code is a defect in the emitter, and `dart format
			// --set-exit-if-changed` over the FIXED FORM's libraries, because
			// that emitter emits the formatter's own shape rather than being
			// reformatted afterwards. The runner is held to both beside them.
			//
			// THE FORMAT CHECK IS SCOPED TO `*Fixed.dart`, exactly as
			// make/dart.mk's tables-dart-fixed-form target scopes it: the
			// packet backend's declaration headers are not format-canonical
			// today, which is a separate emitter item and not this leg's to
			// gate on — this leg measures the fixed form and holds the fixed
			// form's emitter to the rule it already keeps.
			dart := dartExecutable()
			gen := filepath.Join("generated", "bench", "paired", "dart")
			leg := filepath.Join("bench", "tables", "dart", "table_main.dart")
			if e := run(dart, "analyze", gen, leg); e != nil {
				return fmt.Errorf("the dart SDK is required for the dart leg (set DART): %w", e)
			}
			fixedLibraries, e := filepath.Glob(filepath.Join(gen, "*Fixed.dart"))
			if e != nil {
				return e
			}
			if len(fixedLibraries) == 0 {
				return fmt.Errorf("no generated Dart fixed-form libraries under %s", gen)
			}
			sort.Strings(fixedLibraries)
			if e := run(append([]string{dart, "format", "--set-exit-if-changed", "--output=none", leg}, fixedLibraries...)...); e != nil {
				return e
			}
			if e := run(dart, "compile", "exe", "-o", binary("table", lang), leg); e != nil {
				return e
			}
			if e := execute(runner("table", lang, "--gate")); e != nil {
				return e
			}
		}
	}
	hostname, _ := os.Hostname()
	info := buildInfo{Date: time.Now().UTC().Format(time.RFC3339), Host: hostname, OS: runtime.GOOS, Arch: runtime.GOARCH, Revision: gitValue(".", "rev-parse", "HEAD"), Dirty: gitValue(".", "status", "--porcelain") != "", Tools: map[string]string{}, Runtimes: map[string]string{}, Binaries: map[string]string{}, Corpora: map[string]string{}, CorpusIDs: map[string]string{}}
	for key, path := range map[string]string{"serialize": ser, "serialize.c": serC, "serialize.go": serGo, "serialize.cs": serCs} {
		info.Runtimes[key] = gitValue(path, "rev-parse", "HEAD")
		if gitValue(path, "diff", "--name-only", "HEAD", "--") != "" {
			info.Runtimes[key] += "-dirty"
		}
	}
	for key, args := range map[string][]string{"c": {cc, "--version"}, "cpp": {cxx, "--version"}, "go": {"go", "version"}, "dotnet": {"dotnet", "--info"}, "cargo": {cargoExecutable(), "--version"}, "rustc": {rustcExecutable(), "--version"}, "java": {javaExecutable(), "--version"}, "node": {nodeExecutable(), "--version"}, "dart": {dartExecutable(), "--version"}} {
		if b, e := capture(command{args: args}); e == nil {
			info.Tools[key] = strings.TrimSpace(string(b))
		}
	}
	if b, e := capture(command{args: []string{"elixir", "--version"}, env: []string{"PATH=" + beamPath() + string(os.PathListSeparator) + os.Getenv("PATH")}}); e == nil {
		info.Tools["elixir"] = strings.TrimSpace(string(b))
	}
	for _, lang := range langs {
		modules, e := generatedModules(lang)
		if e != nil {
			return e
		}
		for _, p := range modules {
			h, e := hashFile(p)
			if e != nil {
				return e
			}
			info.Binaries[p] = h
		}
		for _, wire := range wiresFor(lang) {
			p := binary(wire, lang)
			h, e := hashFile(p)
			if e != nil {
				return e
			}
			info.Binaries[p] = h
			if lang == "elixir" {
				// EVERY INPUT INDIVIDUALLY, not only the manifest: an edit to a
				// leg script or a regenerated module after the build is what
				// readBuild has to refuse, and it can only refuse a path it
				// recorded.
				inputs, e := elixirInputs(wire)
				if e != nil {
					return e
				}
				for _, in := range inputs {
					h, e := hashFile(in)
					if e != nil {
						return e
					}
					info.Binaries[in] = h
				}
			}
			if lang == "cs" {
				for _, suffix := range []string{".deps.json", ".runtimeconfig.json"} {
					config := strings.TrimSuffix(p, ".dll") + suffix
					hash, err := hashFile(config)
					if err != nil {
						return err
					}
					info.Binaries[config] = hash
				}
			}
		}
	}
	info.CPU = cpuName()
	if version, err := capture(command{args: []string{"uname", "-sr"}}); err == nil {
		info.OSVersion = strings.TrimSpace(string(version))
	} else {
		info.OSVersion = runtime.GOOS
	}

	info.Environment = map[string]string{}
	for _, key := range measuredEnvironment {
		info.Environment[key] = os.Getenv(key)
	}
	info.Tools["cpp_packet_flags"] = strings.Join(cppFlags, " ") + " -DSERIALIZE_RELEASE"
	info.Tools["cpp_table_flags"] = strings.Join(cppFlags, " ") + " -DBENCH_MATCHED"
	info.Tools["c_packet_flags"] = strings.Join(cFlags, " ")
	info.Tools["c_table_flags"] = strings.Join(cFlags, " ") + " -DBENCH_MATCHED -ffp-contract=off"
	if contains(langs, "rust") {
		// The Rust leg's "flags" are cargo settings; the level is also stamped
		// into the binary, so a row's `opt` column and this line agree or the
		// build is not the one that ran.
		info.Tools["rust_table_flags"] = "cargo build --release; BENCH_OPT=" + opt + " CARGO_PROFILE_RELEASE_OPT_LEVEL=" + strings.TrimPrefix(opt, "O")
		info.Runtimes["serialize.rs"] = gitValue(abs(setting("SERIALIZE_RS", "../serialize.rs")), "rev-parse", "HEAD")
		if gitValue(abs(setting("SERIALIZE_RS", "../serialize.rs")), "diff", "--name-only", "HEAD", "--") != "" {
			info.Runtimes["serialize.rs"] += "-dirty"
		}
	}
	if contains(langs, "java") {
		// The Java leg's compile is the JDK's own, and what decides its code is
		// the JIT, which the tools map above carries by version.
		info.Tools["java_table_flags"] = "javac --release 17 -Xlint:all -Werror; java default (JIT, no -ea)"
	}
	if contains(langs, "js") {
		// An interpreted leg has no flags to record; what decides its code is the
		// interpreter, which the tools map above already carries by version.
		info.Tools["js_table_flags"] = "none: node runs the generated ES modules as written"
	}
	if contains(langs, "dart") {
		// The Dart leg's compile is the SDK's own and takes no operator-visible
		// optimization level; what decides its code is the AOT compiler, which
		// the tools map above carries by version.
		info.Tools["dart_table_flags"] = "dart compile exe (AOT); dart analyze and dart format --set-exit-if-changed gate the unit"
	}
	hash, err := hashFile("build/paired/corpus")
	if err != nil {
		return err
	}
	info.Binaries["build/paired/corpus"] = hash
	for _, p := range []string{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema", "testdata/wire/bench_mixed.bin", "bench/corpus/variants/bench_mixed.variants.bin", "bench/paired/corpus/bench_table.bin", "bench/paired/corpus/bench_table.variants.bin", "bench/paired/corpus/bench_table.lengths", "bench/paired/corpus/bench_fixed.bin", "bench/paired/corpus/bench_fixed.layout"} {
		h, e := hashFile(p)
		if e != nil {
			return e
		}
		info.Corpora[p] = h
	}
	for _, wire := range []string{"packet", "table"} {
		id, e := corpusID(wire)
		if e != nil {
			return e
		}
		info.CorpusIDs[wire] = id
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	return os.WriteFile("build/paired/build.json", append(b, '\n'), 0644)
}
func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}
func checkBuildRevision(recorded, current string) error {
	if current == "unavailable" || current == "" || recorded != current {
		return errors.New("source HEAD changed since build; rebuild before reuse")
	}
	return nil
}

func readBuild() (buildInfo, error) {
	var info buildInfo
	b, e := os.ReadFile("build/paired/build.json")
	if e != nil {
		return info, e
	}
	if e = json.Unmarshal(b, &info); e != nil {
		return info, e
	}
	if err := checkBuildRevision(info.Revision, gitValue(".", "rev-parse", "HEAD")); err != nil {
		return info, err
	}
	hostname, _ := os.Hostname()
	if info.Host != hostname || info.Arch != runtime.GOARCH || info.OS != runtime.GOOS {
		return info, errors.New("build belongs to another host or architecture; rebuild here")
	}
	for key, want := range info.Environment {
		if os.Getenv(key) != want {
			return info, fmt.Errorf("runtime setting %s changed since build", key)
		}
	}
	for p, want := range info.Binaries {
		got, e := hashFile(p)
		if e != nil || got != want {
			return info, fmt.Errorf("built executable changed: %s", p)
		}
	}
	for p, want := range info.Corpora {
		got, e := hashFile(p)
		if e != nil || got != want {
			return info, fmt.Errorf("built corpus changed: %s", p)
		}
	}
	return info, nil
}
func gate(langs []string) error {
	if e := run("build/paired/corpus", "verify"); e != nil {
		return e
	}
	for _, lang := range langs {
		for _, wire := range wiresFor(lang) {
			out, e := capture(runner(wire, lang, "--gate"))
			if e != nil {
				return fmt.Errorf("%s/%s gate: %w", lang, wire, e)
			}
			if len(bytes.TrimSpace(out)) != 0 {
				return fmt.Errorf("%s/%s gate emitted stdout; expected no timing rows", lang, wire)
			}
		}
	}
	return nil
}

type sample struct {
	cols        []string
	rate, bytes float64
	iters       int64
}

func parseRows(data []byte, lang, wire, id string) ([]sample, error) {
	iterations := int64(4000000)
	if wire == "table" {
		iterations = 400000
	}
	return parseRowsForIterations(data, lang, wire, id, iterations, 0.2)
}

func parseRowsForIterations(data []byte, lang, wire, id string, wantIterations int64, minimumSeconds float64) ([]sample, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	rows := []sample{}
	seen := map[string]bool{}
	for {
		cols, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(cols) == 0 || strings.HasPrefix(cols[0], "#") || cols[0] == "lang" {
			continue
		}
		if len(cols) != 17 || cols[0] != lang {
			return nil, fmt.Errorf("invalid %s/%s CSV row", lang, wire)
		}
		if cols[1] == "bitpacker" && wire == "packet" {
			continue
		}
		bench, family := "bench_mixed", "gen"
		if wire == "table" {
			bench, family = "bench_table", "table"
		}
		// THE TABLE WIRE HAS TWO FORMS AND ONE CORPUS (docs/SPEC-TABLES.md
		// §3.4). A leg that measures the FIXED form names its rows bench_fixed
		// and a leg that measures the tolerant one names them bench_table;
		// both carry the same 64 logical records, both are family `table`, and
		// both are answerable to the same corpus id, which is what makes the
		// two divisible against one packet row. A leg that emitted BOTH would
		// be a duplicate path and is refused below by `seen`.
		if wire == "table" && cols[1] == "bench_fixed" {
			bench = "bench_fixed"
		}
		if cols[1] != bench || cols[12] != family || cols[11] != id || cols[5] != "1" || (cols[2] != "write" && cols[2] != "round_trip") || seen[cols[2]] {
			return nil, fmt.Errorf("wrong or duplicate %s/%s row identity: %v", lang, wire, cols)
		}
		if cols[14] != expectedChecks(lang, wire) {
			return nil, fmt.Errorf("unexpected %s/%s checks axis %q; expected %q", lang, wire, cols[14], expectedChecks(lang, wire))
		}
		seen[cols[2]] = true
		rate, e := strconv.ParseFloat(cols[8], 64)
		if e != nil || math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
			return nil, errors.New("invalid rate")
		}
		n, e := strconv.ParseFloat(cols[4], 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
			return nil, errors.New("invalid bytes/op")
		}
		iters, e := strconv.ParseInt(cols[3], 10, 64)
		if e != nil || iters != wantIterations {
			return nil, fmt.Errorf("iterations must match requested count %d", wantIterations)
		}
		if float64(iters)/rate < minimumSeconds {
			return nil, fmt.Errorf("%s/%s/%s measured run below 200ms", lang, wire, cols[2])
		}
		if cols[6] != cols[8] || cols[7] != cols[8] {
			return nil, errors.New("per-round rates must agree")
		}
		rows = append(rows, sample{cols: cols, rate: rate, bytes: n, iters: iters})
	}
	if len(rows) != 2 {
		return nil, fmt.Errorf("%s/%s must emit exactly one write and round_trip row", lang, wire)
	}
	return rows, nil
}

// These captions use the same checks semantics as bench/tools/relative.go.
// Refuse changed axes rather than silently reusing a caption for different work.
const checksCaption = "Checks differ: packet C/C++/C# removes bounds/range checks; packet Go always checks; table keeps wire/API validation."
const checksDetails = "Packet C/C++/C# uses `checks=removed`: debug asserts and bounds/range checks compile out. Packet Go uses `checks=always`: bounds, range and sticky-error checks in every build by contract. Every table leg uses `checks=contract`: debug asserts compile out; wire/API contract validation stays in every build. These deliberately labelled cross-checks ratios compare each wire's fastest correct implementation, not identical validation work. The driver refuses any different checks axis."

func expectedChecks(lang, wire string) string {
	if wire == "table" {
		return "contract"
	}
	switch lang {
	case "go":
		return "always"
	case "elixir":
		// The generated Elixir writer takes no caller-error checks and its
		// reader validates the wire contract in every build — there is no
		// release mode that removes either, so the axis is `contract` on both
		// wires (bench/elixir/runner.exs states the same).
		return "contract"
	}
	return "removed"
}

func measure(langs []string, out string, rounds int, info buildInfo, receipt string) (resultErr error) {
	if strings.TrimSpace(receipt) == "" {
		return errors.New("run requires -quiet-window with the operator’s READY/START receipt or reference")
	}
	if rounds < 7 {
		return errors.New("a published paired pass requires at least 7 rounds")
	}
	if out == "" {
		return errors.New("run requires -out (a new results directory)")
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		return errors.New("refusing to overwrite results directory")
	}
	if e := gate(langs); e != nil {
		return e
	}
	// A completed pass appears only after every required leg and round passes.
	parent := filepath.Dir(out)
	if e := os.MkdirAll(parent, 0755); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(parent, ".paired-pass-")
	if e != nil {
		return e
	}
	// Keep failed evidence in its temporary directory; never publish it as results.
	fmt.Fprintln(os.Stderr, "raw pass evidence:", tmp)
	info.Rounds = rounds
	b, _ := json.MarshalIndent(info, "", "  ")
	if e = os.WriteFile(filepath.Join(tmp, "build.json"), append(b, '\n'), 0644); e != nil {
		return e
	}
	window, err := beginWindow(tmp, receipt)
	if err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished {
			resultErr = window.finish(resultErr)
		}
	}()
	control := func(label string) (map[string]float64, error) {
		rates := map[string]float64{}
		for _, wire := range []string{"packet", "table"} {
			output, runErr := window.capture(runner(wire, "cpp", "--csv", "--round", "0"), "control-"+label+"-"+wire)
			writeErr := os.WriteFile(filepath.Join(tmp, "control-"+label+"-"+wire+".csv"), output, 0644)
			if err := errors.Join(runErr, writeErr); err != nil {
				return nil, err
			}
			rows, err := parseRows(output, "cpp", wire, info.CorpusIDs[wire])
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				rates[wire+"/"+row.cols[2]] = row.rate
			}
		}
		return rates, nil
	}
	before, err := control("start")
	if err != nil {
		return err
	}
	for round := range rounds {
		for i, lang := range langs {
			wires := []string{"packet", "table"}
			if (round+i)%2 == 1 {
				wires = []string{"table", "packet"}
			}
			for _, wire := range wires {
				fmt.Fprintf(os.Stderr, "round %d: %s/%s\n", round, lang, wire)
				output, runErr := window.capture(runner(wire, lang, "--csv", "--round", strconv.Itoa(round)), fmt.Sprintf("round-%d-%s-%s", round, lang, wire))
				path := filepath.Join(tmp, fmt.Sprintf("round-%d-%s-%s.csv", round, lang, wire))
				writeErr := os.WriteFile(path, output, 0644)
				if e := errors.Join(runErr, writeErr); e != nil {
					return e
				}
				if _, e = parseRows(output, lang, wire, info.CorpusIDs[wire]); e != nil {
					return e
				}
			}
		}
	}
	after, err := control("end")
	if err != nil {
		return err
	}
	for key, start := range before {
		if math.Abs(after[key]/start-1) > 0.05 {
			return fmt.Errorf("reference C++ %s control moved more than 5%%; repeat the complete sitting", key)
		}
	}
	finished = true
	if err = window.finish(nil); err != nil {
		return err
	}
	loads := make([]map[string]string, 0, len(window.evidence.Samples))
	for _, sample := range window.evidence.Samples {
		loads = append(loads, map[string]string{"date": sample.Date, "phase": sample.Phase, "load": sample.Load})
	}
	b, _ = json.MarshalIndent(loads, "", "  ")
	if e = os.WriteFile(filepath.Join(tmp, "load.json"), append(b, '\n'), 0644); e != nil {
		return e
	}
	if e := renderReport(tmp, false); e != nil {
		return e
	}
	if e := sealPass(tmp); e != nil {
		return e
	}
	if e := render(tmp); e != nil {
		return e
	}
	if e = os.Rename(tmp, out); e != nil {
		return e
	}
	fmt.Fprintln(os.Stderr, "completed paired pass:", out)
	return nil
}
func checkTwinTolerance(best map[string]float64) error {
	if twinTolerance <= 0 {
		return nil
	}
	for _, wire := range []string{"packet", "table"} {
		for _, path := range []string{"write", "round_trip"} {
			c, cpp := best["c/"+wire+"/"+path], best["cpp/"+wire+"/"+path]
			if c <= 0 || cpp <= 0 {
				continue
			}
			hi, lo := c, cpp
			if cpp > hi {
				hi, lo = cpp, c
			}
			dev := (hi/lo - 1) * 100
			if dev > twinTolerance {
				return fmt.Errorf("c and cpp %s/%s differ by %.1f%% > twin-tolerance %.0f%%", wire, path, dev, twinTolerance)
			}
		}
	}
	return nil
}

func render(dir string) error {
	if err := verifyPass(dir); err != nil {
		return err
	}
	return renderReport(dir, true)
}

func renderReport(dir string, writeOutputs bool) error {
	var info buildInfo
	b, e := os.ReadFile(filepath.Join(dir, "build.json"))
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &info); e != nil {
		return e
	}
	if info.Rounds < 7 {
		return errors.New("pass metadata must declare at least 7 rounds")
	}
	for _, wire := range []string{"packet", "table"} {
		controls := map[string]float64{}
		for _, label := range []string{"start", "end"} {
			data, err := os.ReadFile(filepath.Join(dir, "control-"+label+"-"+wire+".csv"))
			if err != nil {
				return err
			}
			rows, err := parseRows(data, "cpp", wire, info.CorpusIDs[wire])
			if err != nil {
				return err
			}
			for _, row := range rows {
				key := row.cols[2]
				if label == "start" {
					controls[key] = row.rate
				} else if math.Abs(row.rate/controls[key]-1) > 0.05 {
					return fmt.Errorf("reference C++ %s/%s control moved more than 5%%", wire, key)
				}
			}
		}
	}
	type result struct {
		first  sample
		rates  []float64
		rounds map[int]bool
	}
	groups := map[string]*result{}
	files, e := filepath.Glob(filepath.Join(dir, "round-*-*-*.csv"))
	if e != nil {
		return e
	}
	for _, p := range files {
		var round int
		var lang, wire string
		parts := strings.Split(strings.TrimSuffix(filepath.Base(p), ".csv"), "-")
		if len(parts) != 4 {
			return fmt.Errorf("unexpected round file: %s", p)
		}
		round, e = strconv.Atoi(parts[1])
		lang, wire = parts[2], parts[3]
		if e != nil || round < 0 || !contains(languages, lang) || (wire != "table" && wire != "packet") {
			return fmt.Errorf("invalid round file: %s", p)
		}
		data, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		rows, e := parseRows(data, lang, wire, info.CorpusIDs[wire])
		if e != nil {
			return e
		}
		for _, s := range rows {
			key := lang + "/" + wire + "/" + s.cols[2]
			g := groups[key]
			if g == nil {
				g = &result{first: s, rounds: map[int]bool{}}
				groups[key] = g
			} else {
				for _, i := range []int{0, 1, 2, 3, 4, 11, 12, 13, 14, 15, 16} {
					if g.first.cols[i] != s.cols[i] {
						return fmt.Errorf("identity changed for %s", key)
					}
				}
			}
			if g.rounds[round] {
				return fmt.Errorf("duplicate round for %s", key)
			}
			g.rounds[round] = true
			g.rates = append(g.rates, s.rate)
		}
	}
	var csvOut, details, readme bytes.Buffer
	fmt.Fprintln(&csvOut, header)
	best := map[string]float64{}
	roundCount := 0
	for _, lang := range languages {
		for _, wire := range []string{"packet", "table"} {
			for _, path := range []string{"write", "round_trip"} {
				key := lang + "/" + wire + "/" + path
				g := groups[key]
				if g == nil || len(g.rates) < 7 {
					return fmt.Errorf("missing 7-round evidence: %s", key)
				}
				if roundCount == 0 {
					roundCount = len(g.rates)
				}
				if len(g.rates) != info.Rounds {
					return errors.New("round count differs from declared pass")
				}
				if len(g.rates) != roundCount {
					return errors.New("round counts differ")
				}
				for i := 0; i < roundCount; i++ {
					if !g.rounds[i] {
						return errors.New("round numbers must be contiguous from 0")
					}
				}
				sort.Float64s(g.rates)
				min, max, median := g.rates[0], g.rates[len(g.rates)-1], g.rates[len(g.rates)/2]
				if len(g.rates)%2 == 0 {
					median = (g.rates[len(g.rates)/2-1] + median) / 2
				}
				spread := (max - min) / median * 100
				if spread > 15 {
					return fmt.Errorf("%s spread %.2f%% exceeds 15%%: repeat the complete pass", key, spread)
				}
				cols := append([]string{}, g.first.cols...)
				cols[5] = strconv.Itoa(len(g.rates))
				cols[6] = fmt.Sprintf("%.0f", median)
				cols[7] = fmt.Sprintf("%.0f", min)
				cols[8] = fmt.Sprintf("%.0f", max)
				cols[9] = fmt.Sprintf("%.2f", median*g.first.bytes/(1024*1024))
				cols[10] = fmt.Sprintf("%.2f", spread)
				fmt.Fprintln(&csvOut, strings.Join(cols, ","))
				best[key] = max
				fmt.Fprintf(&details, "| %s | %s | %s | %.6f | %.6f | %.2f%% | %.6f |\n", names[lang], wire, path, 1e6/max, 1e6/median, spread, g.first.bytes)
			}
		}
	}
	fastest := 0.0
	for _, lang := range languages {
		fastest = math.Max(fastest, best[lang+"/table/round_trip"])
	}
	fmt.Fprintln(&readme, "# Fixed Table\n\n| Language | Fixed Table % | vs Packet Wire % |\n|---|---:|---:|")
	for _, lang := range languages {
		table, packet := best[lang+"/table/round_trip"], best[lang+"/packet/round_trip"]
		fmt.Fprintf(&readme, "| %s | %.0f%% | %.0f%% |\n", names[lang], fastest/table*100, packet/table*100)
	}
	fmt.Fprintln(&readme, "\nFastest table = 100%. Each language’s packet wire = 100%; below 100% beats packet. Lower is better.")
	fmt.Fprintln(&readme, "\n"+checksCaption)
	text := fmt.Sprintf("# Fixed Table details\n\n%s / %s, %s. Revision `%s` (dirty: %t). %d interleaved rounds, one discarded warmup per path per round. Separate standard packet storage and table closure storage carry the same 64 logical records. Table byte counts are exact averages; record padding is not serialized.\n\nHeadline percentages use the best measured round-trip rate, following packet README convention. Seven rounds intentionally follow that convention, with a 4/3 wire-order imbalance; no additional attempts are selected from the separate bracketing controls. Each per-path warmup uses the full 4,000,000 packet or 400,000 table iterations. Public table Load restores declared defaults inside the clock; the runner adds no separate reset. Packet round trips do not require that reset. Before timing, two complete corpus rotations load into one reused target without caller resets and must re-save the exact expected bytes, including the last-to-first transition. Round trip includes read and write; dividing both by two leaves both ratios unchanged. Fixed Table %% = fastest table rate / this table rate × 100; vs Packet Wire %% = this packet rate / this table rate × 100.\n\n| Language | Wire | Path | Best µs/op | Median µs/op | Spread | Mean bytes/op |\n|---|---|---|---:|---:|---:|---:|\n", info.OS, info.Arch, info.Host, info.Revision, info.Dirty, roundCount) + details.String() + "\n" + checksDetails + "\n\n`build.json` records compiler/runtime and binary/corpus identity. `round-*.csv` contains every measured sample; `load.json` records load. `window.json` records the operator READY/START receipt, process names/PIDs/CPU, periodic and boundary samples, and the complete OK verdict. Known foreign build/benchmark processes refuse the sitting; ordinary desktop load is recorded without a universal threshold. Process sampling can miss brief or unusually named work, so the receipt and bracketing controls remain necessary. The shared producer verified packet -> table -> packet identity for all 64 records before any clock. `completion.json` binds the complete raw evidence by SHA-256; rendering verifies it without requiring old local binaries. This detects changed evidence, not a deliberately dishonest operator.\n"
	if err := checkTwinTolerance(best); err != nil {
		return err
	}
	if !writeOutputs {
		return nil
	}
	for p, b := range map[string][]byte{"README.md": readme.Bytes(), "DETAILS.md": []byte(text), "results.csv": csvOut.Bytes()} {
		if e := os.WriteFile(filepath.Join(dir, p), b, 0644); e != nil {
			return e
		}
	}
	return nil
}
func main() {
	mode := flag.String("mode", "gate", "build, gate, fast diagnostic, table report, run confirmation, or render")
	out := flag.String("out", "", "new results directory (or existing directory for render)")
	langsFlag := flag.String("langs", "cpp,c,go,cs", "comma-separated required build/gate languages")
	rounds := flag.Int("rounds", 7, "interleaved measured rounds")
	reuse := flag.Bool("reuse-build", false, "use previously hashed binaries and corpus")
	receipt := flag.String("quiet-window", "", "operator READY/START receipt or reference, required for run")
	fastRounds := flag.Int("fast-rounds", 1, "complete diagnostic rounds (1..3), fast mode only")
	packetIters := flag.Int64("packet-iters", 2000000, "initial Packet iterations per warmup/sample, fast mode only")
	tableIters := flag.Int64("table-iters", 200000, "initial Table iterations per warmup/sample, fast mode only")
	fastTimeout := flag.Duration("fast-timeout", 5*time.Minute, "whole fast-mode deadline, at most 5m including gates")
	noise := flag.String("noise-note", "uncontrolled diagnostic; no quiet-window claim", "operator context recorded by fast mode")
	flag.Float64Var(&twinTolerance, "twin-tolerance", 10, "C and C++ rows of the same wire/path must agree within this percent; 0 disables")
	// Recorded, never applied: the caller pins the sitting (bench/paired/nine.sh
	// runs the whole pass under `bench-lane <core>` so every runner inherits it)
	// and this only carries that fact into the rendered header.
	lane := flag.String("lane", "", "core this sitting was pinned to, recorded in the table header, table mode only")
	flag.Parse()
	fail := func(e error) { fmt.Fprintln(os.Stderr, "paired:", e); os.Exit(1) }
	if _, e := os.Stat("bench/corpus/Bench.schema"); e != nil {
		fail(errors.New("run from repository root"))
	}
	langs := strings.Split(*langsFlag, ",")
	seen := map[string]bool{}
	for _, lang := range langs {
		if !contains(allLanguages(), lang) || seen[lang] {
			fail(errors.New("langs must be unique " + strings.Join(allLanguages(), ",") + " names"))
		}
		seen[lang] = true
	}
	if e := checkRequestShape(*mode, langs); e != nil {
		fail(e)
	}
	if *mode == "render" {
		if e := render(*out); e != nil {
			fail(e)
		}
		return
	}
	// Discovery answers before any build, because the wrapper uses it to
	// decide what to build.
	if *mode == "table-langs" {
		if e := reportTableLanguages(); e != nil {
			fail(e)
		}
		return
	}
	if *mode != "build" && *mode != "gate" && *mode != "run" && *mode != "fast" && *mode != "table" && *mode != "table-langs" {
		fail(errors.New("unknown mode"))
	}
	if *lane != "" && *mode != "table" {
		fail(errors.New("-lane is recorded by table mode only"))
	}
	if *mode == "run" && strings.TrimSpace(*receipt) == "" {
		fail(errors.New("run requires -quiet-window with the operator’s READY/START receipt or reference"))
	}
	fast := fastConfig{Rounds: *fastRounds, PacketIterations: *packetIters, TableIterations: *tableIters, Timeout: *fastTimeout, Noise: *noise}
	if *mode == "fast" {
		// FAST IS THE DIAGNOSTIC MODE. EITHER the whole published paired set —
		// the only shape that yields the board's ratio — OR one unpublished leg
		// on its own: paired (its own packet ratio, no board) or table-only (no
		// ratio at all). It publishes nothing and seals nothing, and it is where
		// a leg that is not in a published pass yet is measured at all.
		// Confirmation is unchanged below: the published four and nothing else.
		if !onlyTableLanguages(langs) && !unpublishedAlone(langs) && len(langs) != len(languages) {
			fail(errors.New("fast mode requires all four paired languages, or one unpublished language alone"))
		}
	}
	// Table mode takes its languages from the tree, not from -langs: the point
	// of the one command is that the same line runs everywhere and the tree
	// answers for what it carries.
	if *mode == "table" {
		if *langsFlag != flag.Lookup("langs").DefValue {
			fail(errors.New("table mode discovers its languages from bench/tables; -langs does not apply"))
		}
	}
	if *mode == "fast" || *mode == "table" {
		if e := fast.validate(); e != nil {
			fail(e)
		}
	}
	if !*reuse && *mode != "fast" && *mode != "table" {
		if e := generateAndBuild(langs); e != nil {
			fail(e)
		}
	}
	info, e := readBuild()
	if e != nil {
		fail(e)
	}
	if *mode == "fast" {
		if e := fastMeasure(langs, *out, info, fast); e != nil {
			fail(e)
		}
		return
	}
	if *mode == "table" {
		if e := tablePass(*out, info, fast, *lane); e != nil {
			fail(e)
		}
		return
	}
	if *mode == "build" {
		return
	}
	if *mode == "gate" {
		if e := gate(langs); e != nil {
			fail(e)
		}
		return
	}
	// A CONFIRMATION PASS IS THE PUBLISHED SET, EXACTLY: provenance.go seals a
	// pass over `languages`, so a pass measuring anything else would render as
	// a pass it is not. The board is a ratio, so a table-only language cannot
	// enter one at all, and an unpublished paired leg is a diagnostic until it
	// is promoted.
	if !slices.Equal(slices.Sorted(slices.Values(langs)), slices.Sorted(slices.Values(languages))) {
		fail(fmt.Errorf("published pass requires exactly the published languages: %s", strings.Join(languages, ",")))
	}
	if e := measure(langs, *out, *rounds, info, *receipt); e != nil {
		fail(e)
	}
}
