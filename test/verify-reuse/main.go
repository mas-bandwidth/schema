// Command verify-reuse runs the Cross-18-Port Dirty-Instance & Sub-Sliced
// Poison-Bracketed Buffer Reuse Gate across all 18 languages:
//  1. Dirty-Instance Reuse (Maximal A -> Minimal B -> Hostile Refused C ->
//     Maximal A -> Minimal B) across Packet wire, Table Form 1 (plain +
//     LoadRetain/SaveRetain reusing the same TableRetain store), Table Form 2
//     (Message batch + Vocabulary), and Table Form 3 (Fixed file + Plan).
//  2. Non-Zero-Offset Sub-Sliced / Poison-Bracketed Reads ([7 x 0xFF] + wire +
//     [13 x 0xFF], or 64-byte aligned [64 x 0xFF] where raw-pointer/aligned
//     Block/Cook Open requires 8/64-byte base alignment) and Writes into
//     [0xA5]-bracketed destination buffers across Packet + Form 1 + Form 2 +
//     Form 3 + Form 4 (Block & Cook), asserting zero canary clobber.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
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

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
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
	matches, _ := filepath.Glob(filepath.Join(repoRoot, pattern))
	for _, m := range slices.Backward(matches) {
		if fileExists(m) {
			return m
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
	want := fmt.Sprintf("REUSE_OK %s 10", lang)
	if err != nil {
		return fmt.Errorf("%s failed (%w):\n%s", lang, err, out)
	}
	if strings.Contains(out, "FAILED:") || !strings.Contains(out, want) {
		return fmt.Errorf("%s did not print %q:\n%s", lang, want, out)
	}
	return nil
}

func generateReuse(repoRoot, schemaBin, schemaMooBin, lang, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	gen := schemaBin
	if lang == "moo" {
		gen = schemaMooBin
	}
	args := []string{"generate", "--lang", lang, "--out", outDir}
	args = append(args, filepath.Join(repoRoot, "test", "verify-reuse", "Reuse.schema"))
	out, err := runChecked(repoRoot, nil, gen, args...)
	if err != nil {
		return fmt.Errorf("generate %s: %w\n%s", lang, err, out)
	}
	return nil
}

func setupGoWorkspace(repoRoot, schemaBin, workDir string) (string, error) {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "go", genDir); err != nil {
		return "", err
	}
	serGo := resolveExistingDir(repoRoot, "SERIALIZE_GO", "../serialize.go", "build/serialize.go")
	shimRoot := filepath.Join(workDir, "schemashim")
	if err := os.MkdirAll(filepath.Join(shimRoot, "tables"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(shimRoot, "go.mod"), []byte("module github.com/mas-bandwidth/schema/v3\n\ngo 1.23\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.Symlink(filepath.Join(repoRoot, "tables", "goruntime"), filepath.Join(shimRoot, "tables", "goruntime")); err != nil && !os.IsExist(err) {
		return "", err
	}
	genMod := fmt.Sprintf(
		"module reusegen\n\ngo 1.23\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v3 v3.0.0-00010101000000-000000000000\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.1.1\n)\n\nreplace github.com/mas-bandwidth/schema/v3 => %s\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n",
		shimRoot, serGo,
	)
	if err := os.WriteFile(filepath.Join(genDir, "go.mod"), []byte(genMod), 0o644); err != nil {
		return "", err
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	appMod := fmt.Sprintf(
		"module reuseapp\n\ngo 1.23\n\nrequire (\n\treusegen v0.0.0\n\tgithub.com/mas-bandwidth/schema/v3 v3.0.0-00010101000000-000000000000\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.1.1\n)\n\nreplace reusegen => ../gen\nreplace github.com/mas-bandwidth/schema/v3 => %s\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n",
		shimRoot, serGo,
	)
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appMod), 0o644); err != nil {
		return "", err
	}
	return appDir, nil
}

func ensureCorpus(repoRoot, schemaBin, workRoot, corpusDir string) error {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return err
	}
	v1Path := filepath.Join(repoRoot, "test", "verify-reuse", "Reuse.schema")
	v2Path := filepath.Join(repoRoot, "test", "verify-reuse", "ReuseV2.schema")

	var comp1, comp2 compiler.Compiler
	u1, err := comp1.Load([]string{v1Path})
	if err != nil || u1 == nil {
		return fmt.Errorf("load v1: %w", err)
	}
	u2, err := comp2.Load([]string{v2Path})
	if err != nil || u2 == nil {
		return fmt.Errorf("load v2: %w", err)
	}
	m1 := tabletext.NewModel(u1)
	m2 := tabletext.NewModel(u2)

	// 1. Form 1 Variable A, Minimal B, Retained A (via V2), and Hostile Refused C
	varAJSON := `{
  "opt_num": 98765,
  "opt_sub": {"code": 42, "title": "opt-sub-title"},
  "label": "maximal-vardoc-label",
  "raw": "AQIDBAUGBwgJCg==",
  "counted": [10, -20, 30, -40],
  "dyn_nums": [100, 200, 300],
  "dyn_subs": [{"code": 1, "title": "s1"}, {"code": 2, "title": "s2"}],
  "scores": {"alpha": 11, "beta": 22, "zeta": 33},
  "choice": {"rich": {"code": 77, "title": "rich-arm"}},
  "perks": ["Read", "Write", "Exec"],
  "note": "blob-note-text",
  "payload": "yv66vg==",
  "child": {"code": 99, "title": "child-node"}
}`
	var rep1 tabletext.Report
	instVarA := m1.New(m1.Lookup("VarDoc"))
	if ok := m1.Read(instVarA, []byte(varAJSON), &rep1); !ok || rep1.Refused {
		return fmt.Errorf("read VarDoc A: rep=%+v", rep1)
	}
	varABytes, err := tablewire.EncodeVariable(m1, instVarA)
	if err != nil {
		return fmt.Errorf("encode VarDoc A: %w", err)
	}

	var repB tabletext.Report
	instVarB := m1.New(m1.Lookup("VarDoc"))
	if ok := m1.Read(instVarB, []byte(`{}`), &repB); !ok || repB.Refused {
		return fmt.Errorf("read VarDoc B: rep=%+v", repB)
	}
	varBBytes, err := tablewire.EncodeVariable(m1, instVarB)
	if err != nil {
		return fmt.Errorf("encode VarDoc B: %w", err)
	}

	varARetainJSON := `{
  "opt_num": 98765,
  "opt_sub": {"code": 42, "title": "opt-sub-title", "future_sub": "opt-ret"},
  "label": "maximal-vardoc-label",
  "raw": "AQIDBAUGBwgJCg==",
  "counted": [10, -20, 30, -40],
  "dyn_nums": [100, 200, 300],
  "dyn_subs": [{"code": 1, "title": "s1", "future_sub": "el0-ret"}, {"code": 2, "title": "s2", "future_sub": "el1-ret"}],
  "scores": {"alpha": 11, "beta": 22, "zeta": 33},
  "choice": {"rich": {"code": 77, "title": "rich-arm", "future_sub": "arm-ret"}},
  "perks": ["Read", "Write", "Exec"],
  "note": "blob-note-text",
  "payload": "yv66vg==",
  "child": {"code": 99, "title": "child-node", "future_sub": "node-ret"},
  "future_tag": "retained-on-root",
  "future_num": "777777"
}`
	var rep2 tabletext.Report
	instVarV2 := m2.New(m2.Lookup("VarDoc"))
	if ok := m2.Read(instVarV2, []byte(varARetainJSON), &rep2); !ok || rep2.Refused {
		return fmt.Errorf("read VarDoc V2: rep=%+v", rep2)
	}
	varARetainBytes, err := tablewire.EncodeVariable(m2, instVarV2)
	if err != nil {
		return fmt.Errorf("encode VarDoc V2: %w", err)
	}

	retInst := m1.New(m1.Lookup("VarDoc"))
	retStore := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 16}
	var retRep tabletext.Report
	if ok, err := tablewire.DecodeRetain(m1, retInst, varARetainBytes, retStore, &retRep); err != nil || !ok || retRep.Retained == 0 {
		return fmt.Errorf("decode retain V1<-V2 failed (ok=%v, rep=%+v): %w", ok, retRep, err)
	}
	varARetainRTBytes, err := tablewire.EncodeRetain(m1, retInst, retStore, &retRep)
	if err != nil {
		return fmt.Errorf("encode retain V1<-V2: %w", err)
	}

	// Hostile Form 1 wire: corrupt "maximal-vardoc-label" byte to invalid UTF-8 0xFF.
	varCBad := bytes.Clone(varABytes)
	idx := bytes.Index(varCBad, []byte("maximal-vardoc-label"))
	if idx < 0 {
		return fmt.Errorf("label not found in varABytes")
	}
	varCBad[idx] = 0xFF

	// 2. Generate Go helper to produce Packet, Message, Fixed (2-row & 1-row), Block, and Cook bytes
	goBuildDir := filepath.Join(workRoot, "corpus-go")
	_ = os.RemoveAll(goBuildDir)
	appDir, err := setupGoWorkspace(repoRoot, schemaBin, goBuildDir)
	if err != nil {
		return err
	}
	helperSrc := `package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	r "reusegen"
	"github.com/mas-bandwidth/serialize.go/v2"
)

func mustWrite(dir, name string, data []byte) {
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		panic(err)
	}
}

func main() {
	dir := os.Args[1]
	varABytes, err := os.ReadFile(filepath.Join(dir, "var_a.bin"))
	if err != nil {
		panic(err)
	}

	// Packet A (Maximal)
	var pktA r.PacketBox
	pktA.Active = true
	pktA.InnerCount = 0x89ABCDEF
	pktA.InnerItems = [4]uint16{111, 222, 333, 444}
	pktA.InnerItemsCount = 4
	pktA.Tags = [4]uint32{1001, 2002, 3003, 4004}
	pktA.TagsCount = 4
	copy(pktA.Label[:], "maximal-packet!")
	pktA.LabelLength = 15
	pktA.Raw = [8]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80}
	pktA.RawLength = 8
	pktA.Pick.Type = r.PktChoiceTypeHeavy
	pktA.Pick.Heavy = r.PktHeavy{X: 0x1122334455667788, Y: -123456}
	pktA.Perks = r.PktFlagsFast | r.PktFlagsSafe | r.PktFlagsLoud
	pktA.Mode = r.PktModeBeta

	bufA := make([]byte, r.PacketBoxMaxBytes)
	wsA := serialize.NewWriteStream(bufA)
	if err := r.WritePacketBox(wsA, &pktA); err != nil {
		panic(err)
	}
	wsA.Flush()
	bitsA := wsA.BitsProcessed()
	bytesA := (bitsA + 7) / 8
	mustWrite(dir, "pkt_a.bin", bufA[:bytesA])
	mustWrite(dir, "pkt_a_bits.txt", []byte(fmt.Sprintf("%d", bitsA)))

	// Hostile Packet C: corrupt "maximal-packet!" with 0xFF invalid UTF-8
	pktCBad := bytes.Clone(bufA[:bytesA])
	pIdx := bytes.Index(pktCBad, []byte("maximal-packet!"))
	if pIdx < 0 {
		panic("packet label not found")
	}
	pktCBad[pIdx] = 0xFF
	mustWrite(dir, "pkt_c_bad.bin", pktCBad)
	mustWrite(dir, "pkt_c_bad_bits.txt", []byte(fmt.Sprintf("%d", bitsA)))

	// Packet B (Minimal)
	var pktB r.PacketBox
	pktB.Active = false
	pktB.Fallback = 42
	pktB.TagsCount = 0
	pktB.LabelLength = 0
	pktB.RawLength = 0
	pktB.Pick.Type = r.PktChoiceTypeVoidArm
	pktB.Perks = 0
	pktB.Mode = r.PktModeAlpha

	bufB := make([]byte, r.PacketBoxMaxBytes)
	wsB := serialize.NewWriteStream(bufB)
	if err := r.WritePacketBox(wsB, &pktB); err != nil {
		panic(err)
	}
	wsB.Flush()
	bitsB := wsB.BitsProcessed()
	bytesB := (bitsB + 7) / 8
	mustWrite(dir, "pkt_b.bin", bufB[:bytesB])
	mustWrite(dir, "pkt_b_bits.txt", []byte(fmt.Sprintf("%d", bitsB)))

	// FixedDoc A & B for Form 2 (Message) and Form 3 (Fixed)
	var fixA r.FixedDoc
	r.FixedDocReset(&fixA)
	fixA.OptVal = 555
	fixA.OptValPresent = true
	fixA.Nums = [4]uint16{10, 20, 30, 40}
	fixA.NumsCount = 4
	copy(fixA.Tag[:], "fixed-maximal")
	fixA.TagLength = 13
	fixA.Raw = [8]byte{9, 8, 7, 6, 5, 0, 0, 0}
	fixA.RawLength = 5
	fixA.Pick.Type = r.FixedChoiceTypePair
	fixA.Pick.Pair().Value = r.FixedPair{A: -11, B: 22}
	fixA.Perks = r.TabFlagsRead | r.TabFlagsExec
	fixA.Mode = r.TabModeBusy

	var fixB r.FixedDoc
	r.FixedDocReset(&fixB)
	fixB.Mode = r.TabModeIdle

	// Announcement + Form 2 Message batches
	ann := make([]byte, r.AnnounceMeasure())
	an := r.Announce(ann)
	mustWrite(dir, "announce.bin", ann[:an])

	var mrep r.TableReport
	msgABuf := make([]byte, 4096)
	mnA := r.FixedDocSaveMessages([]*r.FixedDoc{&fixA, &fixB}, msgABuf, &mrep)
	if mnA <= 0 {
		panic("FixedDocSaveMessages A failed")
	}
	mustWrite(dir, "msg_a.bin", msgABuf[:mnA])

	msgBBuf := make([]byte, 4096)
	mnB := r.FixedDocSaveMessages([]*r.FixedDoc{&fixB}, msgBBuf, &mrep)
	if mnB <= 0 {
		panic("FixedDocSaveMessages B failed")
	}
	mustWrite(dir, "msg_b.bin", msgBBuf[:mnB])

	// Hostile Form 2 Message C: form=2, count=2, then truncated EOF
	mustWrite(dir, "msg_c_bad.bin", []byte{0x02, 0x00, 0xFF})

	// Form 3 Fixed files
	fixABuf := make([]byte, r.FixedDocFixedMeasure(2))
	fnA := r.FixedDocFixedSave([]r.FixedDoc{fixA, fixB}, fixABuf)
	if fnA <= 0 {
		panic("FixedDocFixedSave A failed")
	}
	mustWrite(dir, "fixed_a.bin", fixABuf[:fnA])

	fixBBuf := make([]byte, r.FixedDocFixedMeasure(1))
	fnB := r.FixedDocFixedSave([]r.FixedDoc{fixB}, fixBBuf)
	if fnB <= 0 {
		panic("FixedDocFixedSave B failed")
	}
	mustWrite(dir, "fixed_b.bin", fixBBuf[:fnB])

	fixCBad := bytes.Clone(fixABuf[:fnA])
	fIdx := bytes.Index(fixCBad, []byte("fixed-maximal"))
	if fIdx < 0 {
		panic("fixed-maximal not found")
	}
	fixCBad[fIdx] = 0xFF
	mustWrite(dir, "fixed_c_bad.bin", fixCBad)

	// Form 4 Block (BlockDoc)
	var storage r.BlockDocBlockStorage
	alloc := r.TableBlockAllocator{
		Alloc: func(n int64) []byte { return make([]byte, n) },
		Free:  func([]byte) {},
	}
	if !storage.Create(alloc) {
		panic("BlockDocBlockStorage.Create failed")
	}
	defer storage.Destroy()
	var blk r.BlockDocBlock
	if !r.BlockDocBlockBegin(&blk, &storage, r.BlockDocCounts{}, nil) {
		panic("BlockDocBlockBegin failed")
	}
	blk.Projection.A = -42
	blk.Projection.B = 0x123456789ABCDEF0
	copy(blk.Projection.Label[:], "block-subslice")
	blk.Projection.LabelLength = 14
	blk.Projection.Nums = [4]uint16{11, 22, 33, 0}
	blk.Projection.NumsCount = 3
	blk.Projection.Perks = r.TabFlagsRead | r.TabFlagsWrite
	blkBytes := unsafe.Slice((*byte)(blk.Base), blk.Bytes)
	mustWrite(dir, "block_a.bin", blkBytes)

	// Form 4 Cook (BlockDoc & VarDoc)
	var bdoc r.BlockDoc
	bdoc.A = -42
	bdoc.B = 0x123456789ABCDEF0
	copy(bdoc.Label[:], "block-subslice")
	bdoc.LabelLength = 14
	bdoc.Nums = [4]uint16{11, 22, 33, 0}
	bdoc.NumsCount = 3
	bdoc.Perks = r.TabFlagsRead | r.TabFlagsWrite
	cbBuf := make([]byte, r.BlockDocCookMeasure(&bdoc))
	if !r.BlockDocCookFrom(&bdoc, cbBuf, r.TableByteOrderLittle) {
		panic("BlockDocCookFrom failed")
	}
	mustWrite(dir, "cook_block_a.bin", cbBuf)

	region := make([]byte, 65536)
	var vrep r.TableReport
	vdoc := r.VarDocLoad(region, varABytes, &vrep)
	if vdoc == nil {
		panic("VarDocLoad failed")
	}
	cvBuf := make([]byte, r.VarDocCookMeasure(vdoc))
	if !r.VarDocCookFrom(vdoc, cvBuf, r.TableByteOrderLittle) {
		panic("VarDocCookFrom failed")
	}
	mustWrite(dir, "cook_var_a.bin", cvBuf)
}
`
	if err := os.WriteFile(filepath.Join(corpusDir, "var_a.bin"), varABytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(corpusDir, "var_b.bin"), varBBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(corpusDir, "var_c_bad.bin"), varCBad, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(corpusDir, "var_a_retain.bin"), varARetainBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(corpusDir, "var_a_retain_rt.bin"), varARetainRTBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), mustFmtGo(helperSrc), 0o644); err != nil {
		return err
	}
	if out, err := runChecked(appDir, nil, "go", "run", ".", corpusDir); err != nil {
		return fmt.Errorf("corpus go helper: %w\n%s", err, out)
	}
	return nil
}

func verifyLang(repoRoot, schemaBin, schemaMooBin, corpusDir, workRoot, lang string) error {
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
		return verifyLuau(repoRoot, schemaBin, corpusDir, workDir)
	case "squirrel":
		return verifySquirrel(repoRoot, schemaBin, corpusDir, workDir)
	case "moo":
		return verifyMoo(repoRoot, schemaMooBin, corpusDir, workDir)
	case "goose":
		return verifyGoose(repoRoot, schemaBin, corpusDir, workDir)
	case "lobster":
		return verifyLobster(repoRoot, schemaBin, corpusDir, workDir)
	default:
		return fmt.Errorf("unknown lang %q", lang)
	}
}

func main() {
	langFlag := flag.String("lang", "", "language to verify (or all)")
	repoFlag := flag.String("repo", ".", "repository root")
	schemaFlag := flag.String("schema", "./bin/schema", "path to bin/schema")
	schemaMooFlag := flag.String("schema-moo", "./build/schema-moo", "path to build/schema-moo")
	workFlag := flag.String("work", "./build/verify-reuse", "working directory")
	corpusFlag := flag.String("corpus", "", "pre-generated corpus directory (optional)")
	genOnlyFlag := flag.Bool("generate-only", false, "only generate reuse corpus")
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

	if *genOnlyFlag {
		if err := ensureCorpus(repoRoot, schemaBin, workRoot, corpusDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if !fileExists(filepath.Join(corpusDir, "cook_var_a.bin")) {
		if err := ensureCorpus(repoRoot, schemaBin, workRoot, corpusDir); err != nil {
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
			if err := verifyLang(repoRoot, schemaBin, schemaMooBin, corpusDir, filepath.Join(workRoot, "runs"), lang); err != nil {
				errs <- err
				return
			}
			fmt.Printf("REUSE_OK %s 10\n", lang)
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

func mustFmtGo(src string) []byte {
	b, err := format.Source([]byte(src))
	if err != nil {
		return []byte(src)
	}
	return b
}
