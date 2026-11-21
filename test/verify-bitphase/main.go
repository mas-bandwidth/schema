// Command verify-bitphase runs Pillar #1: the 0..63 packet bit-phase sweep
// across all 18 ports (cpp, c, cs, unity, go, rust, java, dart, elixir, js,
// python, ruby, lua, luau, squirrel, moo, goose, lobster).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var allLangs = []string{
	"cpp", "c", "cs", "unity", "go", "rust", "java", "dart", "elixir",
	"js", "python", "ruby", "lua", "luau", "squirrel", "moo", "goose", "lobster",
}

type manifestCase struct {
	Kind string // "valid" or "refuse"
	Type string // "EmptyZero", "Phase0", ..., "Phase63"
	File string
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
	want := fmt.Sprintf("BITPHASE_OK %s %d", lang, len(requiredRoster()))
	if err != nil {
		return fmt.Errorf("%s failed (%w):\n%s", lang, err, out)
	}
	if strings.Contains(out, "FAILED:") || !strings.Contains(out, want) {
		return fmt.Errorf("%s did not print %q:\n%s", lang, want, out)
	}
	return nil
}

func ensureCorpus(repoRoot, schemaBin, corpusDir, oracleBin string) ([]manifestCase, map[string][]byte, error) {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return nil, nil, err
	}
	cppGen := filepath.Join(filepath.Dir(oracleBin), "cpp-gen")
	if err := os.MkdirAll(cppGen, 0o755); err != nil {
		return nil, nil, err
	}
	schemaFile := filepath.Join(repoRoot, "test", "verify-bitphase", "BitPhase.schema")
	if out, err := runChecked(repoRoot, nil, schemaBin, "generate", "--lang", "cpp", "--out", cppGen, schemaFile); err != nil {
		return nil, nil, fmt.Errorf("generate cpp BitPhase: %w\n%s", err, out)
	}
	serDir := resolveExistingDir(repoRoot, "SERIALIZE", "../serialize", "build/serialize")
	cxx := envOr("CXX", "c++")
	oracleSrc := filepath.Join(repoRoot, "test", "verify-bitphase", "cpp", "oracle.cpp")
	if out, err := runChecked(repoRoot, nil, cxx, "-std=c++17", "-O2", "-Wall", "-Wextra", "-Werror", "-ffp-contract=off",
		"-I"+serDir, "-I"+cppGen, oracleSrc, "-o", oracleBin); err != nil {
		return nil, nil, fmt.Errorf("compile oracle.cpp: %w\n%s", err, out)
	}
	if out, err := runChecked(repoRoot, nil, oracleBin, "--generate", corpusDir); err != nil {
		return nil, nil, fmt.Errorf("oracle --generate: %w\n%s", err, out)
	}
	return loadManifest(corpusDir)
}

// bitphasePhases are the bit phases BitPhase.schema declares a packet for.
var bitphasePhases = []int{0, 1, 7, 31, 32, 33, 56, 63}

// requiredRoster is the corpus the verifier owes, row by row: the 0-bit
// EmptyZero pair, and for every phase three valid variants and the five
// hostile inputs (a flipped reserved, const, align and trailing-pad bit, and a
// trailing byte). It is written here, not read from the manifest the C++
// oracle writes (test/verify-bitphase/cpp/oracle.cpp), so that the manifest is
// held to a roster it did not produce: 66 rows that are not THESE rows is not
// the corpus, and that used to pass on the count alone (F-043: 66 copies of
// the first EmptyZero row printed BITPHASE_OK).
func requiredRoster() []manifestCase {
	out := []manifestCase{
		{"valid", "EmptyZero", "valid_empty.bin"},
		{"refuse", "EmptyZero", "hostile_empty_pad.bin"},
	}
	for _, k := range bitphasePhases {
		typ := fmt.Sprintf("Phase%d", k)
		for v := range 3 {
			out = append(out, manifestCase{"valid", typ, fmt.Sprintf("valid_p%d_v%d.bin", k, v)})
		}
		for _, kind := range []string{"reserved", "const", "align", "trailpad", "trailbyte"} {
			out = append(out, manifestCase{"refuse", typ, fmt.Sprintf("hostile_%s_p%d.bin", kind, k)})
		}
	}
	return out
}

// loadManifest reads the corpus and holds it to requiredRoster by IDENTITY:
// every required row is there exactly once, no other row is, no line is
// malformed, and no two rows carry the same bytes (a file copied over another
// is one vector, not two).
func loadManifest(corpusDir string) ([]manifestCase, map[string][]byte, error) {
	mraw, err := os.ReadFile(filepath.Join(corpusDir, "manifest.tsv"))
	if err != nil {
		return nil, nil, err
	}
	required := map[manifestCase]bool{}
	for _, c := range requiredRoster() {
		required[c] = true
	}
	var cases []manifestCase
	blobs := make(map[string][]byte)
	seen := map[manifestCase]bool{}
	sameBytes := map[string]string{}
	var bad []string
	for n, line := range strings.Split(strings.TrimSpace(string(mraw)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 3 {
			bad = append(bad, fmt.Sprintf("line %d is not kind, type, file: %q", n+1, line))
			continue
		}
		c := manifestCase{Kind: fields[0], Type: fields[1], File: fields[2]}
		switch {
		case !required[c]:
			bad = append(bad, fmt.Sprintf("line %d (%s %s %s) is not a row of the required roster", n+1, c.Kind, c.Type, c.File))
			continue
		case seen[c]:
			bad = append(bad, fmt.Sprintf("line %d repeats %s %s %s", n+1, c.Kind, c.Type, c.File))
			continue
		}
		seen[c] = true
		b, err := os.ReadFile(filepath.Join(corpusDir, c.File))
		if err != nil {
			return nil, nil, err
		}
		if other, dup := sameBytes[string(b)]; dup {
			bad = append(bad, fmt.Sprintf("%s carries the same bytes as %s: one vector, counted twice", c.File, other))
		}
		sameBytes[string(b)] = c.File
		cases = append(cases, c)
		blobs[c.File] = b
	}
	for _, c := range requiredRoster() {
		if !seen[c] {
			bad = append(bad, fmt.Sprintf("the required row %s %s %s is missing", c.Kind, c.Type, c.File))
		}
	}
	if len(bad) > 0 {
		return nil, nil, fmt.Errorf("%s is not the bitphase corpus (%d required rows, %d distinct ones read):\n  %s",
			filepath.Join(corpusDir, "manifest.tsv"), len(required), len(cases), strings.Join(bad, "\n  "))
	}
	return cases, blobs, nil
}

func generateBitPhase(repoRoot, schemaBin, schemaMooBin, lang, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	gen := schemaBin
	if lang == "moo" {
		gen = schemaMooBin
	}
	schemaFile := filepath.Join(repoRoot, "test", "verify-bitphase", "BitPhase.schema")
	out, err := runChecked(repoRoot, nil, gen, "generate", "--lang", lang, "--out", outDir, schemaFile)
	if err != nil {
		return fmt.Errorf("generate %s: %w\n%s", lang, err, out)
	}
	return nil
}

func verifyLang(repoRoot, schemaBin, schemaMooBin, oracleBin, corpusDir, workRoot, lang string, cases []manifestCase, blobs map[string][]byte) error {
	workDir := filepath.Join(workRoot, lang)
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	switch lang {
	case "cpp":
		out, err := runChecked(repoRoot, nil, oracleBin, "--verify", corpusDir)
		return requireToken(out, err, "cpp")
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
		return verifyLuau(repoRoot, schemaBin, workDir, cases, blobs)
	case "squirrel":
		return verifySquirrel(repoRoot, schemaBin, corpusDir, workDir, cases)
	case "moo":
		return verifyMoo(repoRoot, schemaMooBin, workDir, cases, blobs)
	case "goose":
		return verifyGoose(repoRoot, schemaBin, corpusDir, workDir, cases)
	case "lobster":
		return verifyLobster(repoRoot, schemaBin, corpusDir, workDir, cases)
	default:
		return fmt.Errorf("unknown lang %q", lang)
	}
}

func main() {
	langFlag := flag.String("lang", "", "language to verify (or 'all')")
	repoFlag := flag.String("repo", ".", "repository root")
	schemaFlag := flag.String("schema", "./bin/schema", "path to bin/schema")
	schemaMooFlag := flag.String("schema-moo", "./build/schema-moo", "path to build/schema-moo")
	workFlag := flag.String("work", "./build/verify-bitphase", "working directory")
	corpusFlag := flag.String("corpus", "", "pre-generated corpus directory (optional)")
	genOnlyFlag := flag.Bool("generate-only", false, "only build oracle and generate corpus")
	flag.Parse()

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
	oracleBin := filepath.Join(workRoot, "oracle")

	var cases []manifestCase
	var blobs map[string][]byte
	if *genOnlyFlag {
		_, _, err = ensureCorpus(repoRoot, schemaBin, corpusDir, oracleBin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	needOracle := *langFlag == "all" || strings.Contains(","+*langFlag+",", ",cpp,")
	if _, err := os.Stat(filepath.Join(corpusDir, "manifest.tsv")); err == nil && (!needOracle || fileExists(oracleBin)) {
		cases, blobs, err = loadManifest(corpusDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		cases, blobs, err = ensureCorpus(repoRoot, schemaBin, corpusDir, oracleBin)
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
			if err := verifyLang(repoRoot, schemaBin, schemaMooBin, oracleBin, corpusDir, filepath.Join(workRoot, "runs"), lang, cases, blobs); err != nil {
				errs <- err
				return
			}
			fmt.Printf("BITPHASE_OK %s %d\n", lang, len(cases))
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

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
