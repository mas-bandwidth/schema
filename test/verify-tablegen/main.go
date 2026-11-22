// Command verify-tablegen generates deterministic adversarial 2-generation
// (V1.schema, V2.schema, V2.schema.lock) schema pairs exercising cross-language
// reserved-word / helper identifier minefields, non-trivial scalar/string/float
// defaults, deeply nested structs-in-unions-in-[Enum]Table-in-map[string]V +
// DAG pointers + Block/Cook roots, and V1<->V2 `was` renames + scalar widenings
// + historical V1 writer-bound refusals, then compiles and differentially
// verifies Form 1, Form 1 Retain, Form 2 Messages (same-version + V1<->V2),
// Form 3 Fixed + schema.lock lineage + writer-bound breach refusal, and JSON
// across all 18 target languages with strict compiler/analyzer flags.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var allLangs = []string{
	"cpp", "c", "cs", "unity", "go", "rust", "java", "dart", "elixir",
	"js", "python", "ruby", "lua", "luau", "squirrel", "moo", "goose", "lobster",
}

// parseTargets is --lang as the target names every later step keys on: "all",
// or a comma list whose names are trimmed and whose empty entries are dropped,
// so `lobster, js` and `lobster,js` name the same two targets everywhere (the
// run, the matrix's bookkeeping and its printout: one spelling, one key).
func parseTargets(flag string) []string {
	if strings.TrimSpace(flag) == "all" {
		return slices.Clone(allLangs)
	}
	var out []string
	for name := range strings.SplitSeq(flag, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

type PairMeta struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	PkgV1     string `json:"pkg_v1"`
	PkgV2     string `json:"pkg_v2"`
	FileBase  string `json:"file_base"`
	DirV1     string `json:"dir_v1"`
	DirV2     string `json:"dir_v2"`
	FixRootV1 string `json:"fix_root_v1"`
	FixRootV2 string `json:"fix_root_v2"`
	BlkRootV1 string `json:"blk_root_v1"`
	BlkRootV2 string `json:"blk_root_v2"`
	VarRootV1 string `json:"var_root_v1"`
	VarRootV2 string `json:"var_root_v2"`
}

type CorpusMeta struct {
	Seed      uint64     `json:"seed"`
	Schemas   int        `json:"schemas"`
	CaseCount int        `json:"case_count"`
	Pairs     []PairMeta `json:"pairs"`
	// Pinned is the hand-written array-default matrix's units (pinned.go)
	Pinned []PinnedUnit `json:"pinned"`
	// PinnedVersion is pinned.go's case layout; a cached corpus of another
	// layout is generated again
	PinnedVersion int `json:"pinned_version"`
	// only, when set by --only, is the packages the kept cases name
	only map[string]bool
}

type UnitSpec struct {
	Pkg       string
	FileBase  string
	SchemaDir string
	VarRoot   string
	FixRoot   string
	BlkRoot   string
}

func unitsFromMeta(meta *CorpusMeta) []UnitSpec {
	out := make([]UnitSpec, 0, len(meta.Pairs)*2)
	for _, p := range meta.Pairs {
		out = append(out,
			UnitSpec{
				Pkg: p.PkgV1, FileBase: p.FileBase, SchemaDir: p.DirV1,
				VarRoot: p.VarRootV1, FixRoot: p.FixRootV1, BlkRoot: p.BlkRootV1,
			},
			UnitSpec{
				Pkg: p.PkgV2, FileBase: p.FileBase, SchemaDir: p.DirV2,
				VarRoot: p.VarRootV2, FixRoot: p.FixRootV2, BlkRoot: p.BlkRootV2,
			},
		)
	}
	for _, p := range meta.Pinned {
		out = append(out, UnitSpec{
			Pkg: p.Pkg, FileBase: p.FileBase, SchemaDir: p.Dir,
			VarRoot: pinnedVarRoot, FixRoot: pinnedFixRoot, BlkRoot: pinnedBlkRoot,
		})
	}
	if meta.only != nil {
		out = slices.DeleteFunc(out, func(u UnitSpec) bool { return !meta.only[u.Pkg] })
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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
	roots := []string{repoRoot}
	if rel, ok := strings.CutPrefix(pattern, "dist/"); ok {
		for _, sdk := range []string{"/opt/nova/sdk", "/home/nova/sdk"} {
			matches, _ := filepath.Glob(filepath.Join(sdk, rel))
			for _, m := range slices.Backward(matches) {
				if !strings.Contains(m, "broken") && fileExists(m) {
					return m
				}
			}
		}
	}
	for _, r := range roots {
		matches, _ := filepath.Glob(filepath.Join(r, pattern))
		for _, m := range slices.Backward(matches) {
			if !strings.Contains(m, "broken") && fileExists(m) {
				return m
			}
		}
	}
	return fallback
}

func resolveSquirrelTool(repoRoot string) string {
	for _, k := range []string{"SQ", "SQUIRREL"} {
		if v := os.Getenv(k); toolUsable(v) {
			return v
		}
	}
	for _, pat := range []string{
		filepath.Join(repoRoot, "dist/squirrel-*/bin/sq"),
		"/opt/nova/sdk/squirrel-*-double/bin/sq",
		"/home/nova/sdk/squirrel-*-double/bin/sq",
		"/opt/nova/sdk/squirrel-*/bin/sq",
		"/home/nova/sdk/squirrel-*/bin/sq",
	} {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if fileExists(m) {
				return m
			}
		}
	}
	return "sq"
}

func resolveMooTool(repoRoot string) string {
	if v := os.Getenv("MOO"); toolUsable(v) {
		return v
	}
	for _, pat := range []string{
		filepath.Join(repoRoot, "dist/moo-*/bin/moo"),
		filepath.Join(repoRoot, "dist/moo-*/moo"),
		"/opt/nova/sdk/moo-*-checked/bin/moo",
		"/opt/nova/sdk/moo-*-checked/moo",
		"/home/nova/sdk/moo-*-checked/bin/moo",
		"/opt/nova/sdk/moo-*/bin/moo",
		"/opt/nova/sdk/moo-*/moo",
		"/home/nova/sdk/moo-*/bin/moo",
	} {
		matches, _ := filepath.Glob(pat)
		for _, m := range slices.Backward(matches) {
			if !strings.Contains(m, "broken") && fileExists(m) {
				return m
			}
		}
	}
	return "moo"
}

func resolveGooseTool(repoRoot string) string {
	if v := os.Getenv("GOOSE"); toolUsable(v) {
		return v
	}
	for _, pat := range []string{
		filepath.Join(repoRoot, "dist/goose-*/goose"),
		filepath.Join(repoRoot, "dist/goose-*/bin/goose"),
		"/opt/nova/sdk/goose-*/goose",
		"/opt/nova/sdk/goose-*/bin/goose",
		"/home/nova/sdk/goose-*/goose",
		"/home/nova/sdk/goose-*/bin/goose",
	} {
		matches, _ := filepath.Glob(pat)
		for _, m := range slices.Backward(matches) {
			if fileExists(m) {
				return m
			}
		}
	}
	return "goose"
}

func runChecked(dir string, extraEnv []string, name string, args ...string) (string, error) {
	run := func() (string, error) {
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
	if matrix != nil {
		if out, err, ok := matrix.viaPack(args, run); ok {
			return out, err
		}
	}
	return run()
}

func requireToken(out string, err error, lang string, caseCount int) error {
	want := fmt.Sprintf("TABLEGEN_OK %s %d", lang, caseCount)
	if err != nil {
		return fmt.Errorf("%s failed (%w):\n%s", lang, err, out)
	}
	if strings.Contains(out, "FAILED:") || !strings.Contains(out, want) {
		return fmt.Errorf("%s did not print %q:\n%s", lang, want, out)
	}
	return nil
}

func generateAllUnits(repoRoot, schemaBin, schemaMooBin, lang string, units []UnitSpec, genRoot string, extraOpts ...string) error {
	if err := os.MkdirAll(genRoot, 0o755); err != nil {
		return err
	}
	gen := schemaBin
	if lang == "moo" {
		gen = schemaMooBin
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	errCh := make(chan error, len(units))
	for _, u := range units {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			outDir := filepath.Join(genRoot, u.Pkg)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				errCh <- err
				return
			}
			args := []string{"generate", "--lang", lang}
			args = append(args, extraOpts...)
			args = append(args, "--out", outDir, u.SchemaDir)
			out, err := runChecked(repoRoot, nil, gen, args...)
			if err != nil {
				errCh <- fmt.Errorf("generate %s (%s): %w\n%s", lang, u.Pkg, err, out)
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return nil
}

func ensureTablegenCorpus(corpusDir string, seed uint64, schemas int, forceGen bool) (*CorpusMeta, []CorpusCase, []byte, error) {
	metaPath := filepath.Join(corpusDir, "meta.json")
	packPath := filepath.Join(corpusDir, "corpus.pack")
	if !forceGen && fileExists(metaPath) && fileExists(packPath) {
		mraw, err1 := os.ReadFile(metaPath)
		praw, err2 := os.ReadFile(packPath)
		if err1 == nil && err2 == nil {
			var meta CorpusMeta
			if json.Unmarshal(mraw, &meta) == nil && meta.Seed == seed && meta.Schemas == schemas && len(meta.Pairs) == schemas && len(meta.Pinned) == len(pinnedShapes()) && meta.PinnedVersion == pinnedVersion {
				allExist := true
				for _, pu := range meta.Pinned {
					if !fileExists(pu.Dir) {
						allExist = false
					}
				}
				for _, pm := range meta.Pairs {
					if !fileExists(pm.DirV1) || !fileExists(pm.DirV2) {
						allExist = false
						break
					}
				}
				if allExist {
					cases, err := UnpackCases(praw)
					if err == nil && len(cases) == meta.CaseCount {
						return &meta, cases, praw, nil
					}
				}
			}
		}
	}

	if err := os.RemoveAll(corpusDir); err != nil {
		return nil, nil, nil, err
	}
	schemasDir := filepath.Join(corpusDir, "schemas")
	pairs, err := GenerateSchemaPairs(schemasDir, seed, schemas)
	if err != nil {
		return nil, nil, nil, err
	}
	cases, _, err := BuildCorpus(pairs, seed, corpusDir)
	if err != nil {
		return nil, nil, nil, err
	}
	pinned, pinnedCases, err := BuildPinned(schemasDir)
	if err != nil {
		return nil, nil, nil, err
	}
	cases = append(cases, pinnedCases...)
	packed := PackCases(cases)
	if err := os.WriteFile(packPath, packed, 0o644); err != nil {
		return nil, nil, nil, err
	}
	meta := &CorpusMeta{
		Seed:          seed,
		Schemas:       schemas,
		CaseCount:     len(cases),
		Pairs:         make([]PairMeta, len(pairs)),
		Pinned:        pinned,
		PinnedVersion: pinnedVersion,
	}
	for i, p := range pairs {
		meta.Pairs[i] = PairMeta{
			Index: p.Index, Name: p.Name, PkgV1: p.PkgV1, PkgV2: p.PkgV2,
			FileBase: p.FileBase, DirV1: p.DirV1, DirV2: p.DirV2,
			FixRootV1: p.FixRootV1, FixRootV2: p.FixRootV2,
			BlkRootV1: p.BlkRootV1, BlkRootV2: p.BlkRootV2,
			VarRootV1: p.VarRootV1, VarRootV2: p.VarRootV2,
		}
	}
	mraw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := os.WriteFile(metaPath, mraw, 0o644); err != nil {
		return nil, nil, nil, err
	}
	return meta, cases, packed, nil
}

func verifyLang(repoRoot, schemaBin, schemaMooBin, corpusDir, workRoot, lang string, meta *CorpusMeta, cases []CorpusCase, packed []byte) error {
	workDir := filepath.Join(workRoot, lang)
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	units := unitsFromMeta(meta)
	packFile := filepath.Join(corpusDir, "corpus.pack")
	switch lang {
	case "cpp":
		return verifyCPP(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "c":
		return verifyC(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "cs":
		return verifyCS(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "unity":
		return verifyUnity(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "go":
		return verifyGo(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "rust":
		return verifyRust(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "java":
		return verifyJava(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "dart":
		return verifyDart(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "elixir":
		return verifyElixir(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "js":
		return verifyJS(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "python":
		return verifyPython(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "ruby":
		return verifyRuby(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "lua":
		return verifyLua(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "luau":
		return verifyLuau(repoRoot, schemaBin, workDir, units, cases)
	case "squirrel":
		return verifySquirrel(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "moo":
		return verifyMoo(repoRoot, schemaMooBin, workDir, units, cases)
	case "goose":
		return verifyGoose(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	case "lobster":
		return verifyLobster(repoRoot, schemaBin, packFile, workDir, units, len(cases))
	default:
		return fmt.Errorf("unknown lang %q", lang)
	}
}

func main() {
	langFlag := flag.String("lang", "", "language to verify (or 'all' or comma-separated)")
	seedFlag := flag.Uint64("seed", 1, "deterministic RNG seed")
	schemasFlag := flag.Int("schemas", 12, "number of (V1, V2, schema.lock) pairs to generate")
	repoFlag := flag.String("repo", ".", "repository root")
	schemaFlag := flag.String("schema", "./bin/schema", "path to bin/schema")
	schemaMooFlag := flag.String("schema-moo", "./build/schema-moo", "path to build/schema-moo")
	workFlag := flag.String("work", "./build/verify-tablegen", "working directory")
	corpusFlag := flag.String("corpus", "", "pre-generated corpus directory (optional)")
	genOnlyFlag := flag.Bool("generate-only", false, "only generate schemas, lockfiles, and oracle corpus")
	matrixFlag := flag.Bool("matrix", false, "name every failing case of each target instead of stopping at the first (a diagnostic, matrix.go)")
	onlyFlag := flag.String("only", "", "run only the cases whose id contains this text (a diagnostic: the gate runs them all)")
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

	meta, cases, packed, err := ensureTablegenCorpus(corpusDir, *seedFlag, *schemasFlag, *genOnlyFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *genOnlyFlag {
		fmt.Printf("TABLEGEN_CORPUS seed=%d pairs=%d cases=%d bytes=%d\n", meta.Seed, meta.Schemas, meta.CaseCount, len(packed))
		return
	}

	if *onlyFlag != "" {
		cases = slices.DeleteFunc(cases, func(c CorpusCase) bool { return !strings.Contains(c.ID, *onlyFlag) })
		if len(cases) == 0 {
			fmt.Fprintf(os.Stderr, "--only %q keeps no case\n", *onlyFlag)
			os.Exit(2)
		}
		meta.only = map[string]bool{}
		for _, c := range cases {
			meta.only[c.Pkg] = true
		}
		packed = PackCases(cases)
		corpusDir = filepath.Join(workRoot, "only")
		if err := os.MkdirAll(corpusDir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(corpusDir, "corpus.pack"), packed, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	targets := parseTargets(*langFlag)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "--lang is required")
		os.Exit(2)
	}

	if *matrixFlag {
		matrix = newMatrix()
		var mwg sync.WaitGroup
		for _, lang := range targets {
			mwg.Go(func() {
				runMatrix(repoRoot, schemaBin, schemaMooBin, workRoot, lang, meta, cases)
			})
		}
		mwg.Wait()
		printMatrix(targets, len(cases))
		return
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(targets))
	for _, lang := range targets {
		wg.Go(func() {
			if err := verifyLang(repoRoot, schemaBin, schemaMooBin, corpusDir, filepath.Join(workRoot, "runs"), lang, meta, cases, packed); err != nil {
				errs <- err
				return
			}
			fmt.Printf("TABLEGEN_OK %s %d\n", lang, len(cases))
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
