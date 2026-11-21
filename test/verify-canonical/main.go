// Command verify-canonical runs Pillar #5 across all 18 ports:
//  1. Maps constructed from sorted, reverse-sorted, and shuffled JSON key orders
//     (map[string]int32 including UTF-16 vs UTF-8 U+FF61/U+1F600 trap, empty key,
//     prefix keys, >16B unbounded key; map[string(16)]int32; map[int64]Child;
//     map[uint64]uint32 above 2^63; enum-keyed [Tier]uint32; nested maps) emit
//     100% byte-identical canonical wire bytes AND canonical JSON bytes.
//  2. Native in-memory construction in sorted, reverse-sorted, and shuffled
//     insertion orders + two separately allocated dynamic equal strings
//     ("shared-" + "blob") and byte buffers ([0xde, 0xad, 0xbe, 0xef]) in
//     *string / *bytes slots emit the exact same canonical wire & JSON bytes.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

var allLangs = []string{
	"cpp", "c", "cs", "unity", "go", "rust", "java", "dart", "elixir",
	"js", "python", "ruby", "lua", "luau", "squirrel", "moo", "goose", "lobster",
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func toolUsable(p string) bool {
	if p == "" {
		return false
	}
	if strings.ContainsRune(p, filepath.Separator) {
		return fileExists(p)
	}
	_, err := exec.LookPath(p)
	return err == nil
}

func resolveExistingDir(repoRoot, envKey, defRel, fallbackRel string) string {
	for _, cand := range []string{os.Getenv(envKey), defRel, fallbackRel} {
		if cand == "" {
			continue
		}
		p := cand
		if !filepath.IsAbs(p) {
			p = filepath.Join(repoRoot, p)
		}
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	fb := filepath.Join(repoRoot, fallbackRel)
	abs, _ := filepath.Abs(fb)
	return abs
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func resolveTool(repoRoot, envKey, distRel, fallback string) string {
	if v := os.Getenv(envKey); toolUsable(v) {
		return v
	}
	cand := filepath.Join(repoRoot, distRel)
	if fileExists(cand) {
		return cand
	}
	return fallback
}

func resolveGlobTool(repoRoot, envKey, pattern, fallback string) string {
	if v := os.Getenv(envKey); toolUsable(v) {
		return v
	}
	matches, _ := filepath.Glob(filepath.Join(repoRoot, pattern))
	for _, matche := range slices.Backward(matches) {
		if fileExists(matche) {
			return matche
		}
	}
	return fallback
}

func runChecked(dir string, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func requireToken(out string, err error, lang string) error {
	want := fmt.Sprintf("CANONICAL_OK %s 6", lang)
	if err != nil {
		return fmt.Errorf("%s failed (%w):\n%s", lang, err, out)
	}
	if strings.Contains(out, "FAILED:") || !strings.Contains(out, want) {
		return fmt.Errorf("%s did not print %q:\n%s", lang, want, out)
	}
	return nil
}

func buildJSONPermutations() (sortedJSON, reverseJSON, shuffledJSON string) {
	// Order 0: canonical sorted
	sortedJSON = `{
  "by_name": {"": 10, "Z": -5, "a": 20, "ab": 0, "abc": -7, "long-unbounded-key-alpha": 12345, "\uff61": 111, "\ud83d\ude00": 222},
  "by_code": {"Alpha": 1, "a": 5, "ab": 4, "beta": 2, "gamma": 3},
  "by_id": {"-9000000000000": {"label": "min-far", "score": -99}, "-1": {"label": "minus-one", "score": -1}, "0": {"label": "", "score": 0}, "2": {"label": "two", "score": 2}, "9000000000000": {"label": "max-far", "score": 99}},
  "by_uid": {"0": 0, "1": 11, "255": 2550, "256": 2560, "9223372036854775807": 700, "9223372036854775808": 800, "18446744073709551614": 900},
  "by_tier": {"Bronze": 100, "Silver": 200, "Gold": 300, "Platinum": 400},
  "nested": {"outer-a": {"-100": -10, "0": 0, "7": 70, "42": 420}, "outer-m": {"-5": 55, "12": 120}, "outer-z": {}},
  "text_a": "shared-blob",
  "text_b": "shared-blob",
  "blob_a": "3q2+7w==",
  "blob_b": "3q2+7w=="
}`
	// Order 1: strictly reversed keys at every map, inner map, enum-keyed array, and top-level field
	reverseJSON = `{
  "blob_b": "3q2+7w==",
  "blob_a": "3q2+7w==",
  "text_b": "shared-blob",
  "text_a": "shared-blob",
  "nested": {"outer-z": {}, "outer-m": {"12": 120, "-5": 55}, "outer-a": {"42": 420, "7": 70, "0": 0, "-100": -10}},
  "by_tier": {"Platinum": 400, "Gold": 300, "Silver": 200, "Bronze": 100},
  "by_uid": {"18446744073709551614": 900, "9223372036854775808": 800, "9223372036854775807": 700, "256": 2560, "255": 2550, "1": 11, "0": 0},
  "by_id": {"9000000000000": {"score": 99, "label": "max-far"}, "2": {"score": 2, "label": "two"}, "0": {"score": 0, "label": ""}, "-1": {"score": -1, "label": "minus-one"}, "-9000000000000": {"score": -99, "label": "min-far"}},
  "by_code": {"gamma": 3, "beta": 2, "ab": 4, "a": 5, "Alpha": 1},
  "by_name": {"\ud83d\ude00": 222, "\uff61": 111, "long-unbounded-key-alpha": 12345, "abc": -7, "ab": 0, "a": 20, "Z": -5, "": 10}
}`
	// Order 2: shuffled keys (putting U+1F600 before U+FF61, "abc" before "ab", 256 before 255, >=2^63 before <2^63, positives before negatives)
	shuffledJSON = `{
  "text_b": "shared-blob",
  "by_uid": {"9223372036854775808": 800, "256": 2560, "0": 0, "18446744073709551614": 900, "255": 2550, "9223372036854775807": 700, "1": 11},
  "by_name": {"\ud83d\ude00": 222, "abc": -7, "a": 20, "\uff61": 111, "": 10, "long-unbounded-key-alpha": 12345, "Z": -5, "ab": 0},
  "blob_a": "3q2+7w==",
  "nested": {"outer-m": {"12": 120, "-5": 55}, "outer-z": {}, "outer-a": {"7": 70, "-100": -10, "42": 420, "0": 0}},
  "by_tier": {"Gold": 300, "Bronze": 100, "Platinum": 400, "Silver": 200},
  "by_id": {"2": {"label": "two", "score": 2}, "-9000000000000": {"label": "min-far", "score": -99}, "9000000000000": {"label": "max-far", "score": 99}, "0": {"label": "", "score": 0}, "-1": {"label": "minus-one", "score": -1}},
  "text_a": "shared-blob",
  "by_code": {"ab": 4, "gamma": 3, "Alpha": 1, "beta": 2, "a": 5},
  "blob_b": "3q2+7w=="
}`
	return
}

func ensureCorpus(repoRoot, corpusDir string) ([]byte, []byte, [3][]byte, error) {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return nil, nil, [3][]byte{}, err
	}
	schemaFile := filepath.Join(repoRoot, "test", "verify-canonical", "Canonical.schema")
	var comp compiler.Compiler
	comp.FormatInPlace = false
	u, err := comp.Load([]string{schemaFile})
	if err != nil || u == nil {
		return nil, nil, [3][]byte{}, fmt.Errorf("load Canonical.schema: %w", err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("Bundle")
	if def == nil {
		return nil, nil, [3][]byte{}, fmt.Errorf("bundle table missing")
	}

	s0, s1, s2 := buildJSONPermutations()
	perms := [3][]byte{[]byte(s0), []byte(s1), []byte(s2)}
	names := permutationNames

	var wantWire, wantJSON []byte
	for i, raw := range perms {
		inst := m.New(def)
		var rep tabletext.Report
		if ok := m.Read(inst, raw, &rep); !ok || rep.Refused {
			return nil, nil, [3][]byte{}, fmt.Errorf("oracle Read %s failed: ok=%v rep=%+v", names[i], ok, rep)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			return nil, nil, [3][]byte{}, fmt.Errorf("oracle Encode %s: %w", names[i], err)
		}
		js, err := m.Write(inst)
		if err != nil {
			return nil, nil, [3][]byte{}, fmt.Errorf("oracle Write %s: %w", names[i], err)
		}
		if len(js) == 0 || js[len(js)-1] != '\n' {
			js = append(js, '\n')
		}
		if i == 0 {
			wantWire = wire
			wantJSON = js
		} else {
			if !bytes.Equal(wire, wantWire) {
				return nil, nil, [3][]byte{}, fmt.Errorf("oracle wire mismatch on %s", names[i])
			}
			if !bytes.Equal(js, wantJSON) {
				return nil, nil, [3][]byte{}, fmt.Errorf("oracle JSON mismatch on %s", names[i])
			}
		}
		if err := os.WriteFile(filepath.Join(corpusDir, names[i]), raw, 0o644); err != nil {
			return nil, nil, [3][]byte{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(corpusDir, "canonical.bin"), wantWire, 0o644); err != nil {
		return nil, nil, [3][]byte{}, err
	}
	if err := os.WriteFile(filepath.Join(corpusDir, "canonical.json"), wantJSON, 0o644); err != nil {
		return nil, nil, [3][]byte{}, err
	}
	return wantWire, wantJSON, perms, nil
}

// permutationNames are the three JSON inputs of the corpus, in the order
// buildJSONPermutations returns them.
var permutationNames = [3]string{"json_sorted.json", "json_reverse.json", "json_shuffled.json"}

// loadCorpus reads a pre-generated corpus and holds it to the corpus this
// program writes, BY IDENTITY: each of the three JSON inputs is, byte for
// byte, the permutation of its name, so the three are three orderings and not
// one input under three names. A leg read "6 of 6" when json_sorted.json had
// been copied over the other two (F-044): the same key order three times is
// one permutation, and the reverse and shuffled orders were never read. A
// file that cannot be read is an error, never an empty input.
func loadCorpus(corpusDir string) (wire, text []byte, perms [3][]byte, err error) {
	read := func(name string) []byte {
		data, rerr := os.ReadFile(filepath.Join(corpusDir, name))
		if rerr != nil && err == nil {
			err = rerr
		}
		if rerr == nil && len(data) == 0 && err == nil {
			err = fmt.Errorf("%s is empty", filepath.Join(corpusDir, name))
		}
		return data
	}
	wire, text = read("canonical.bin"), read("canonical.json")
	s0, s1, s2 := buildJSONPermutations()
	want := [3]string{s0, s1, s2}
	var bad []string
	for i, name := range permutationNames {
		perms[i] = read(name)
		if err != nil {
			return nil, nil, [3][]byte{}, err
		}
		if string(perms[i]) != want[i] {
			which := "another input"
			for j, w := range want {
				if j != i && string(perms[i]) == w {
					which = permutationNames[j] + "'s"
				}
			}
			bad = append(bad, fmt.Sprintf("%s does not carry its own ordering (it carries %s): the permutations are checked by identity, and a copy is one ordering counted twice", name, which))
		}
	}
	if err != nil {
		return nil, nil, [3][]byte{}, err
	}
	if len(bad) > 0 {
		return nil, nil, [3][]byte{}, fmt.Errorf("%s is not the canonical corpus:\n  %s", corpusDir, strings.Join(bad, "\n  "))
	}
	return wire, text, perms, nil
}

func generateCanonical(repoRoot, schemaBin, schemaMooBin, lang, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	gen := schemaBin
	if lang == "moo" {
		gen = schemaMooBin
	}
	args := []string{"generate", "--lang", lang}
	if lang == "python" {
		args = append(args, "--surface", "wire,fixed,json,cook,block,view,retain,textform")
	}
	args = append(args, "--out", outDir, filepath.Join(repoRoot, "test", "verify-canonical", "Canonical.schema"))
	out, err := runChecked(repoRoot, nil, gen, args...)
	if err != nil {
		return fmt.Errorf("generate %s: %w\n%s", lang, err, out)
	}
	return nil
}

func verifyLang(repoRoot, schemaBin, schemaMooBin, corpusDir, workRoot, lang string, wantWire, wantJSON []byte, perms [3][]byte) error {
	workDir := filepath.Join(workRoot, lang)
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	switch lang {
	case "cpp":
		return verifyCPP(repoRoot, schemaBin, corpusDir, workDir)
	case "c":
		return verifyC(repoRoot, schemaBin, corpusDir, workDir)
	case "cs":
		return verifyCS(repoRoot, schemaBin, corpusDir, workDir)
	case "unity":
		return verifyUnity(repoRoot, schemaBin, corpusDir, workDir)
	case "go":
		return verifyGo(repoRoot, schemaBin, corpusDir, workDir)
	case "rust":
		return verifyRust(repoRoot, schemaBin, corpusDir, workDir)
	case "java":
		return verifyJava(repoRoot, schemaBin, corpusDir, workDir)
	case "dart":
		return verifyDart(repoRoot, schemaBin, corpusDir, workDir)
	case "elixir":
		return verifyElixir(repoRoot, schemaBin, corpusDir, workDir)
	case "js":
		return verifyJS(repoRoot, schemaBin, corpusDir, workDir)
	case "python":
		return verifyPython(repoRoot, schemaBin, corpusDir, workDir)
	case "ruby":
		return verifyRuby(repoRoot, schemaBin, corpusDir, workDir)
	case "lua":
		return verifyLua(repoRoot, schemaBin, corpusDir, workDir)
	case "luau":
		return verifyLuau(repoRoot, schemaBin, workDir, wantWire, wantJSON, perms)
	case "squirrel":
		return verifySquirrel(repoRoot, schemaBin, corpusDir, workDir)
	case "moo":
		return verifyMoo(repoRoot, schemaMooBin, workDir, wantWire, wantJSON, perms)
	case "goose":
		return verifyGoose(repoRoot, schemaBin, corpusDir, workDir)
	case "lobster":
		return verifyLobster(repoRoot, schemaBin, corpusDir, workDir)
	default:
		return fmt.Errorf("unknown lang %q", lang)
	}
}

func main() {
	langFlag := flag.String("lang", "", "language to verify (or 'all')")
	repoFlag := flag.String("repo", ".", "repository root")
	schemaFlag := flag.String("schema", "./bin/schema", "path to bin/schema")
	schemaMooFlag := flag.String("schema-moo", "./build/schema-moo", "path to build/schema-moo")
	workFlag := flag.String("work", "./build/verify-canonical", "working directory")
	corpusFlag := flag.String("corpus", "", "pre-generated corpus directory (optional)")
	corpusCasesFlag := flag.String("corpus-cases", "", "replay this committed folder of corpus cases and exit (testdata/corpus-cases)")
	genOnlyFlag := flag.Bool("generate-only", false, "only generate canonical corpus")
	flag.Parse()
	if *corpusCasesFlag != "" {
		scratch, err := os.MkdirTemp("", "canonical-corpus-cases-")
		if err == nil {
			err = runCorpusCases(*corpusCasesFlag, scratch)
			_ = os.RemoveAll(scratch)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	repoRoot, err := filepath.Abs(*repoFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	schemaBin := *schemaFlag
	if !filepath.IsAbs(schemaBin) {
		schemaBin = filepath.Join(repoRoot, schemaBin)
	}
	schemaMooBin := *schemaMooFlag
	if !filepath.IsAbs(schemaMooBin) {
		schemaMooBin = filepath.Join(repoRoot, schemaMooBin)
	}
	workRoot := *workFlag
	if !filepath.IsAbs(workRoot) {
		workRoot = filepath.Join(repoRoot, workRoot)
	}
	corpusDir := *corpusFlag
	if corpusDir == "" {
		corpusDir = filepath.Join(workRoot, "corpus")
	} else if !filepath.IsAbs(corpusDir) {
		corpusDir = filepath.Join(repoRoot, corpusDir)
	}

	var wantWire, wantJSON []byte
	var perms [3][]byte
	if *genOnlyFlag {
		_, _, _, err = ensureCorpus(repoRoot, corpusDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if fileExists(filepath.Join(corpusDir, "canonical.bin")) && fileExists(filepath.Join(corpusDir, "json_shuffled.json")) {
		wantWire, wantJSON, perms, err = loadCorpus(corpusDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		wantWire, wantJSON, perms, err = ensureCorpus(repoRoot, corpusDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	var targets []string
	switch {
	case *langFlag == "all":
		targets = allLangs
	case *langFlag != "":
		targets = strings.Split(*langFlag, ",")
	default:
		fmt.Fprintln(os.Stderr, "--lang is required")
		os.Exit(2)
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(targets))
	for _, lang := range targets {
		lang := strings.TrimSpace(lang)
		wg.Go(func() {
			if err := verifyLang(repoRoot, schemaBin, schemaMooBin, corpusDir, filepath.Join(workRoot, "runs"), lang, wantWire, wantJSON, perms); err != nil {
				errs <- err
				return
			}
			fmt.Printf("CANONICAL_OK %s 6\n", lang)
		})
	}
	wg.Wait()
	close(errs)
	failed := false
	for err := range errs {
		fmt.Fprintln(os.Stderr, err)
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}
