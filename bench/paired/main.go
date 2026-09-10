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

var languages = []string{"cpp", "c", "go", "cs"}
var names = map[string]string{"c": "C", "cpp": "C++", "go": "Go", "cs": "C#"}

// twinTolerance is the C vs C++ paired-row band (percent). Rows within it
// do not fail the twin check. 0 disables. Flag --twin-tolerance.
var twinTolerance = 10.0

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
}

func execute(c command) error {
	fmt.Fprintln(os.Stderr, "+", c.args[0], strings.Join(c.args[1:], " "))
	cmd := exec.Command(c.args[0], c.args[1:]...)
	cmd.Dir = c.dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func capture(c command) ([]byte, error) {
	cmd := exec.Command(c.args[0], c.args[1:]...)
	cmd.Dir = c.dir
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
	p := filepath.Join("build", "paired", wire+"-"+lang)
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
func runner(wire, lang string, args ...string) command {
	a := []string{binary(wire, lang)}
	if lang == "cs" {
		a = append([]string{"dotnet"}, a...)
	}
	if wire == "table" {
		a = append(a, "--indexed", "--wire-dir", "bench/paired/corpus", "--variant-dir", "bench/paired/corpus")
	} else {
		a = append(a, "--wire-dir", "testdata/wire", "--variant-dir", "bench/corpus/variants")
	}
	return command{args: append(a, args...)}
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
		if e := run(schema, "generate", "--lang", lang, "--out", "generated/bench/paired/"+lang, "bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"); e != nil {
			return e
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
			a = append(a, "-ffp-contract=off", "-DBENCH_MATCHED", "-Igenerated/bench/paired/c", "-I"+serC, "bench/tables/c/table_main.c", "-o", binary("table", lang), "-lm")
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
	for key, args := range map[string][]string{"c": {cc, "--version"}, "cpp": {cxx, "--version"}, "go": {"go", "version"}, "dotnet": {"dotnet", "--info"}} {
		if b, e := capture(command{args: args}); e == nil {
			info.Tools[key] = strings.TrimSpace(string(b))
		}
	}
	for _, lang := range langs {
		for _, wire := range []string{"packet", "table"} {
			p := binary(wire, lang)
			h, e := hashFile(p)
			if e != nil {
				return e
			}
			info.Binaries[p] = h
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
		for _, wire := range []string{"packet", "table"} {
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
	if lang == "go" {
		return "always"
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
				return fmt.Errorf("C and C++ %s/%s differ by %.1f%% > twin-tolerance %.0f%%", wire, path, dev, twinTolerance)
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
	mode := flag.String("mode", "gate", "build, gate, fast diagnostic, run confirmation, or render")
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
	flag.Parse()
	fail := func(e error) { fmt.Fprintln(os.Stderr, "paired:", e); os.Exit(1) }
	if _, e := os.Stat("bench/corpus/Bench.schema"); e != nil {
		fail(errors.New("run from repository root"))
	}
	langs := strings.Split(*langsFlag, ",")
	seen := map[string]bool{}
	for _, lang := range langs {
		if !contains(languages, lang) || seen[lang] {
			fail(errors.New("langs must be unique c,cpp,go,cs names"))
		}
		seen[lang] = true
	}
	if *mode == "render" {
		if e := render(*out); e != nil {
			fail(e)
		}
		return
	}
	if *mode != "build" && *mode != "gate" && *mode != "run" && *mode != "fast" {
		fail(errors.New("unknown mode"))
	}
	if *mode == "run" && strings.TrimSpace(*receipt) == "" {
		fail(errors.New("run requires -quiet-window with the operator’s READY/START receipt or reference"))
	}
	fast := fastConfig{Rounds: *fastRounds, PacketIterations: *packetIters, TableIterations: *tableIters, Timeout: *fastTimeout, Noise: *noise}
	if *mode == "fast" {
		if len(langs) != 4 {
			fail(errors.New("fast mode requires all four languages"))
		}
		if e := fast.validate(); e != nil {
			fail(e)
		}
	}
	if !*reuse && *mode != "fast" {
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
	if *mode == "build" {
		return
	}
	if *mode == "gate" {
		if e := gate(langs); e != nil {
			fail(e)
		}
		return
	}
	if len(langs) != 4 {
		fail(errors.New("published pass requires all four languages"))
	}
	if e := measure(langs, *out, *rounds, info, *receipt); e != nil {
		fail(e)
	}
}
