package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageFuzzForm is the wire-fuzz pipe roster form byte used exclusively by
// `conformance-harness message-fuzz`. Every mutant frame sent on this form is:
//
//	[u32 LE announceLen][announceBytes (announceLen bytes)][batchBytes]
//
// When announceLen == 0, the leg resolves batchBytes against its unit's own
// compiled announcement (fast path). When announceLen > 0, the leg resolves a
// fresh Vocabulary via AnnounceRead(announceBytes) first (refusing the mutant
// if AnnounceRead refuses or damages), then runs LoadMessages + SaveMessages
// in the reader unit's own canonical vocabulary.
const messageFuzzForm byte = 5

type messageFuzzOptions struct {
	driver     string
	only       string
	work       string
	failed     string
	replay     string
	unit       string
	root       string
	jsonDir    string
	seed       uint64
	n          int
	maxMutants int
}

type messageFuzzTarget struct {
	unit string
	root string
}

var messageFuzzTargets = []messageFuzzTarget{
	{unit: "backenddemo", root: "LoginRequest"},
	{unit: "backenddemo", root: "MatchResult"},
	{unit: "backenddemo", root: "StorePurchase"},
	{unit: "backenddemo", root: "Envelope"},
	{unit: "vocabdemo", root: "Wide00"},
	{unit: "vocabdemo", root: "Wide09"},
	{unit: "vocab9demo", root: "Wide00"},
	{unit: "vocab9demo", root: "Wide19"},
	{unit: "messagedemo", root: "ToolMessage"},
	{unit: "tblvarevo1", root: "EvoMsg"},
	{unit: "tblvarevo2", root: "EvoMsg"},
	{unit: "tblvarevo3", root: "EvoMsg"},
	{unit: "tblfixedevo", root: "EvoRoot"},
	{unit: "tabledemo", root: "RootConfig"},
	{unit: "tabledemo", root: "ProfileConfig"},
	{unit: "tabledemo", root: "KeyedConfig"},
	{unit: "tblv1", root: "Cfg"},
	{unit: "tblv2", root: "Cfg"},
	{unit: "tblm1", root: "Msg"},
	{unit: "tblm2", root: "Msg"},
	{unit: "tbla1", root: "Root"},
	{unit: "tbla2", root: "Root"},
	{unit: "tblk1", root: "Root"},
	{unit: "tblk2", root: "Root"},
	{unit: "scalars", root: "SimState"},
	{unit: "tblscalars2", root: "SimState"},
}

type messageCrossPair struct {
	writerUnit string
	readerUnit string
	root       string
}

var messageCrossPairs = []messageCrossPair{
	{writerUnit: "tblvarevo1", readerUnit: "tblvarevo3", root: "EvoMsg"},
	{writerUnit: "tblvarevo3", readerUnit: "tblvarevo1", root: "EvoMsg"},
	{writerUnit: "tblvarevo2", readerUnit: "tblvarevo3", root: "EvoMsg"},
	{writerUnit: "tblvarevo1", readerUnit: "tblvarevo2", root: "EvoMsg"},
	{writerUnit: "tblvarevo3", readerUnit: "tblvarevo2", root: "EvoMsg"},
	{writerUnit: "tblv1", readerUnit: "tblv2", root: "Cfg"},
	{writerUnit: "tblv2", readerUnit: "tblv1", root: "Cfg"},
	{writerUnit: "tblm1", readerUnit: "tblm2", root: "Msg"},
	{writerUnit: "tblm2", readerUnit: "tblm1", root: "Msg"},
	{writerUnit: "tbla1", readerUnit: "tbla2", root: "Root"},
	{writerUnit: "tbla2", readerUnit: "tbla1", root: "Root"},
	{writerUnit: "tblk1", readerUnit: "tblk2", root: "Root"},
	{writerUnit: "tblk2", readerUnit: "tblk1", root: "Root"},
	{writerUnit: "scalars", readerUnit: "tblscalars2", root: "SimState"},
	{writerUnit: "tblscalars2", readerUnit: "scalars", root: "SimState"},
}

type messageFuzzRoot struct {
	unit       string
	root       string
	model      *tabletext.Model
	def        *ir.Struct
	announce   []byte
	vocabulary *tablewire.Vocabulary
	entries    []ir.TableVocabularyEntry
}

type messageFuzzCase struct {
	name     string
	rootIdx  int
	announce []byte
	batch    []byte
}

func packMessageFuzzFrame(announce, batch []byte) []byte {
	out := make([]byte, 4+len(announce)+len(batch))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(announce)))
	copy(out[4:4+len(announce)], announce)
	copy(out[4+len(announce):], batch)
	return out
}

func unpackMessageFuzzFrame(frame []byte) (announce, batch []byte, ok bool) {
	if len(frame) < 4 {
		return nil, nil, false
	}
	aLen := int(binary.LittleEndian.Uint32(frame[0:4]))
	if aLen < 0 || 4+aLen > len(frame) {
		return nil, nil, false
	}
	return frame[4 : 4+aLen], frame[4+aLen:], true
}

func (r *messageFuzzRoot) oracle(frame []byte) (ans oracleAnswer, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("message-fuzz oracle panicked on %s.%s: %v", r.unit, r.root, p)
		}
	}()
	announceBytes, batchBytes, ok := unpackMessageFuzzFrame(frame)
	if !ok {
		ans.report = Counts{Refused: true}
		return ans, nil
	}
	vocab := r.vocabulary
	if len(announceBytes) > 0 {
		var custom tablewire.Vocabulary
		var arep tabletext.Report
		aerr := custom.AnnounceRead(announceBytes, &arep)
		if aerr != nil || !strictReportClean(&arep) || !custom.Announced() {
			ans.report = Counts{Refused: true}
			return ans, nil
		}
		vocab = &custom
	}
	bodies, _ := tablewire.MessageCount(batchBytes, vocab)
	if bodies < 1 {
		bodies = 1
	}
	if bodies > tablewire.TableMessageBatchMax {
		bodies = tablewire.TableMessageBatchMax
	}
	batch := make([]*tabletext.Instance, bodies)
	for i := range batch {
		batch[i] = r.model.New(r.def)
	}
	var rep tabletext.Report
	n, decOK, derr := tablewire.DecodeMessages(r.model, batch, batchBytes, vocab, &rep)
	strictOK := strictReportClean(&rep) && strictMessageSkipDepthOK(r.def, vocab, batchBytes)
	if !strictOK || derr != nil || !decOK || n < 1 {
		ans.report = Counts{Unknown: rep.Unknown, Widened: rep.Widened, Refused: true}
		return ans, nil
	}
	encoded, eerr := tablewire.EncodeMessages(r.model, batch[:n])
	if eerr != nil {
		return ans, fmt.Errorf("oracle could not re-encode accepted %s.%s batch (%d bodies): %w", r.unit, r.root, n, eerr)
	}
	ans.accepted = true
	ans.report = Counts{Unknown: rep.Unknown, Widened: rep.Widened}
	ans.encoded = encoded
	return ans, nil
}

func resolveMessageFuzzDriver(opts messageFuzzOptions) (string, []string, error) {
	if opts.driver != "" {
		return opts.driver, nil, nil
	}
	lang := strings.TrimSpace(opts.only)
	if lang == "" {
		return "", nil, errors.New("message-fuzz needs --driver <cmd> or --only <lang>")
	}
	envOr := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	switch lang {
	case "cpp":
		return "./build/wire-fuzz-cpp", nil, nil
	case "c":
		return "./build/wire-fuzz-c", nil, nil
	case "cs":
		dotnet := envOr("DOTNET", "dotnet")
		return dotnet + " test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz", nil, nil
	case "unity":
		return "test/conformance/unity/driver wire-fuzz", nil, nil
	case "go":
		return "./build/conformance-go wire-fuzz", nil, nil
	case "rust":
		return "./build/conformance-rust-all", nil, nil
	case "java":
		javaBin := envOr("JAVA", "java")
		return javaBin + " -cp build/conformance-java TablesWireFuzz", nil, nil
	case "dart":
		return "./build/conformance-dart wire-fuzz", nil, nil
	case "elixir":
		wd, _ := os.Getwd()
		return "test/conformance/elixir/driver wire-fuzz", []string{"ELIXIR_TABLES_EBIN=" + filepath.Join(wd, "build/elixir-tables-ebin")}, nil
	case "js":
		return "test/conformance/js/driver wire-fuzz", nil, nil
	case "python":
		py := envOr("PYTHON", "python3")
		return py + " -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python", nil, nil
	case "ruby":
		rb := envOr("RUBY", "ruby")
		return rb + " --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby", nil, nil
	case "lua":
		luaBin := envOr("LUA", "lua5.4")
		return luaBin + " test/conformance/lua/messagefuzz.lua build/conformance-lua", nil, nil
	case "luau":
		return "test/conformance/luau/messagefuzz-driver", []string{"LUAUFUZZ_NOTE=/dev/null"}, nil
	case "squirrel":
		sq := os.Getenv("SQ")
		if sq == "" {
			sq = "dist/squirrel/bin/sq"
			if _, err := os.Stat(sq); err != nil {
				sq = "/home/nova/sdk/squirrel-3.2-double/bin/sq"
			}
		}
		return sq + " test/conformance/squirrel/messagefuzz.nut", nil, nil
	case "moo":
		return "test/conformance/moo/wirefuzz-driver", []string{"MOOFUZZ_NOTE=/dev/null"}, nil
	case "goose":
		dispatch := envOr("GOOSE_DISPATCH", "./build/goose-wirefuzz-dispatch")
		legs := envOr("GOOSE_MESSAGEFUZZ_LEGS", "build/goose-messagefuzz-legs/legs")
		return dispatch + " " + legs, nil, nil
	case "lobster":
		host := envOr("LOBSTER_WIRE_FUZZ_HOST", "./build/lobster-wirefuzzhost build/conformance-lobster file")
		return host, nil, nil
	default:
		return "", nil, fmt.Errorf("unknown --only language %q", lang)
	}
}

func startMessageFuzzLeg(command string, extraEnv []string, roots []*messageFuzzRoot) (*wireLeg, error) {
	wireRoots := make([]*wireRoot, len(roots))
	for i, r := range roots {
		wireRoots[i] = &wireRoot{
			unit: r.unit,
			root: r.root,
		}
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, errors.New("--driver names no command")
	}
	// startWireLeg sends form=1/2; here we send messageFuzzForm (5) so
	// wire-fuzz's form-1/form-2 absent lists remain untouched.
	return startCustomFormWireLeg(parts, extraEnv, wireRoots, messageFuzzForm)
}

func spawnWireLegProcess(parts []string, extraEnv []string) (*wireLeg, error) {
	cmd := exec.Command(parts[0], parts[1:]...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	leg := &wireLeg{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}
	cmd.Stderr = &leg.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", strings.Join(parts, " "), err)
	}
	return leg, nil
}

func startCustomFormWireLeg(parts []string, extraEnv []string, roots []*wireRoot, form byte) (*wireLeg, error) {
	leg, err := spawnWireLegProcess(parts, extraEnv)
	if err != nil {
		return nil, err
	}
	var roster bytes.Buffer
	_ = binary.Write(&roster, binary.LittleEndian, uint32(len(roots)))
	for _, r := range roots {
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(r.unit)))
		roster.WriteString(r.unit)
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(r.root)))
		roster.WriteString(r.root)
		roster.WriteByte(form)
		roster.WriteByte(0)
	}
	if _, err := leg.stdin.Write(roster.Bytes()); err != nil {
		leg.kill()
		return nil, fmt.Errorf("the leg closed its input during the message-fuzz roster: %w\n%s", err, leg.stderr.String())
	}
	leg.known = make([]bool, len(roots))
	for i := range roots {
		b, err := leg.stdout.ReadByte()
		if err != nil {
			leg.kill()
			return nil, fmt.Errorf("the leg did not answer the message-fuzz roster at %d/%d: %w\n%s", i, len(roots), err, leg.stderr.String())
		}
		leg.known[i] = b != 0
	}
	return leg, nil
}

func messageFuzz(w io.Writer, m *Manifest, opts messageFuzzOptions) error {
	start := time.Now()
	driverCmd, extraEnv, err := resolveMessageFuzzDriver(opts)
	if err != nil {
		return fmt.Errorf("message-fuzz: %w", err)
	}
	n := opts.n
	if n < 0 {
		return errors.New("message-fuzz: --n must not be negative")
	}
	if opts.n == 0 && opts.replay == "" {
		n = 1500
	}

	u := newUnits(m)
	roots, rootMap, err := buildMessageFuzzRoots(u, opts)
	if err != nil {
		return fmt.Errorf("message-fuzz: %w", err)
	}

	leg, err := startMessageFuzzLeg(driverCmd, extraEnv, roots)
	if err != nil {
		return fmt.Errorf("message-fuzz: %w", err)
	}
	defer func() { _ = leg.close() }()

	knownCount := 0
	for _, k := range leg.known {
		if k {
			knownCount++
		}
	}
	if knownCount == 0 {
		leg.kill()
		return fmt.Errorf("message-fuzz: driver %q answered 0 for all %d Form-2 roots", driverCmd, len(roots))
	}

	if opts.replay != "" {
		if opts.unit == "" || opts.root == "" {
			return errors.New("message-fuzz: --replay requires --unit and --root")
		}
		ri, ok := rootMap[opts.unit+"."+opts.root]
		if !ok || !leg.known[ri] {
			return fmt.Errorf("message-fuzz: driver has no Form-2 codec for %s.%s", opts.unit, opts.root)
		}
		frame, rerr := os.ReadFile(opts.replay)
		if rerr != nil {
			return rerr
		}
		if err := checkOneMessageMutant(leg, roots, ri, filepath.Base(opts.replay), frame, opts.failed); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "message-fuzz: replayed %s on %s.%s: 0 divergences\n", opts.replay, opts.unit, opts.root)
		return nil
	}

	systematic, seedBatches, err := buildMessageFuzzCorpus(u, m, roots, rootMap, leg.known, opts.jsonDir, opts.seed)
	if err != nil {
		return fmt.Errorf("message-fuzz: %w", err)
	}

	rng := rand.New(rand.NewPCG(opts.seed, opts.seed^0x6a09e667f3bcc908))
	// Deterministic shuffle of systematic cases so --max-mutants still samples
	// every root & category evenly if set, while full runs execute 100% of them.
	perm := rng.Perm(len(systematic))

	total := 0
	accepted := 0
	sysRun := 0
	for _, idx := range perm {
		if opts.maxMutants > 0 && total >= opts.maxMutants {
			break
		}
		tc := systematic[idx]
		if !leg.known[tc.rootIdx] {
			continue
		}
		frame := packMessageFuzzFrame(tc.announce, tc.batch)
		ok, cerr := runAndCompareMessageMutant(leg, roots, tc.rootIdx, tc.name, frame, opts.failed)
		if cerr != nil {
			return cerr
		}
		if ok {
			accepted++
		}
		total++
		sysRun++
	}

	randRun := 0
	for randRun < n {
		if opts.maxMutants > 0 && total >= opts.maxMutants {
			break
		}
		tc := randomMessageMutant(rng, roots, leg.known, seedBatches, randRun)
		frame := packMessageFuzzFrame(tc.announce, tc.batch)
		ok, cerr := runAndCompareMessageMutant(leg, roots, tc.rootIdx, tc.name, frame, opts.failed)
		if cerr != nil {
			return cerr
		}
		if ok {
			accepted++
		}
		total++
		randRun++
	}

	elapsed := time.Since(start)
	rate := float64(total) / max(elapsed.Seconds(), 0.001)
	_, _ = fmt.Fprintf(w, "message-fuzz: seed %d, %d/%d roots live, %d systematic + %d random = %d mutants (%d accepted), 0 divergences, %.2f s, %.0f mutants/s\n",
		opts.seed, knownCount, len(roots), sysRun, randRun, total, accepted, elapsed.Seconds(), rate)
	return nil
}

func checkOneMessageMutant(leg *wireLeg, roots []*messageFuzzRoot, ri int, label string, frame []byte, failedPath string) error {
	_, err := runAndCompareMessageMutant(leg, roots, ri, label, frame, failedPath)
	return err
}

func runAndCompareMessageMutant(leg *wireLeg, roots []*messageFuzzRoot, ri int, label string, frame []byte, failedPath string) (bool, error) {
	r := roots[ri]
	ans, err := r.oracle(frame)
	if err != nil {
		return false, err
	}
	if err := leg.send(ri, frame); err != nil {
		saveFailedMessageFrame(failedPath, frame)
		leg.kill()
		return false, fmt.Errorf("message-fuzz: %s (%s.%s): leg closed pipe on send: %w\n%s", label, r.unit, r.root, err, leg.stderr.String())
	}
	rep, err := leg.receiveWithDeadline(realClock{})
	if err != nil {
		saveFailedMessageFrame(failedPath, frame)
		leg.kill()
		return false, fmt.Errorf("message-fuzz: %s (%s.%s): leg did not answer: %w\n%s", label, r.unit, r.root, err, leg.stderr.String())
	}
	legAccepted := rep.accepted()
	if legAccepted != ans.accepted {
		saveFailedMessageFrame(failedPath, frame)
		ann, batch, _ := unpackMessageFuzzFrame(frame)
		return false, fmt.Errorf(
			"message-fuzz DIVERGENCE on %s (%s.%s): verdict differs\n  leg:    accepted=%v loaded=%v malformed=%v refused=%v report=%s\n  oracle: accepted=%v report=%s\n  announce (%d B): %x\n  batch    (%d B): %x",
			label, r.unit, r.root,
			legAccepted, rep.loaded, rep.report.Malformed, rep.report.Refused, rep.report.String(),
			ans.accepted, ans.report.String(),
			len(ann), truncateHex(ann, 96), len(batch), truncateHex(batch, 96),
		)
	}
	if ans.accepted {
		if rep.saveFail || !bytes.Equal(rep.saved, ans.encoded) {
			saveFailedMessageFrame(failedPath, frame)
			ann, batch, _ := unpackMessageFuzzFrame(frame)
			diffAt := firstDiffOffset(ans.encoded, rep.saved)
			return false, fmt.Errorf(
				"message-fuzz DIVERGENCE on %s (%s.%s): canonical re-encoded batch differs at byte %d\n  leg saved    (%d B, saveFail=%v): %x\n  oracle saved (%d B):             %x\n  announce (%d B): %x\n  input batch (%d B): %x",
				label, r.unit, r.root, diffAt,
				len(rep.saved), rep.saveFail, truncateHex(rep.saved, 128),
				len(ans.encoded), truncateHex(ans.encoded, 128),
				len(ann), truncateHex(ann, 96), len(batch), truncateHex(batch, 96),
			)
		}
	}
	return ans.accepted, nil
}

func truncateHex(b []byte, limit int) []byte {
	if len(b) <= limit {
		return b
	}
	return b[:limit]
}

func firstDiffOffset(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func saveFailedMessageFrame(path string, frame []byte) {
	if path == "" {
		return
	}
	_ = writeFileAtomic(path, frame)
}

func buildMessageFuzzRoots(u *units, opts messageFuzzOptions) ([]*messageFuzzRoot, map[string]int, error) {
	targets := messageFuzzTargets
	if opts.replay != "" && opts.unit != "" && opts.root != "" {
		found := false
		for _, t := range targets {
			if t.unit == opts.unit && t.root == opts.root {
				found = true
				break
			}
		}
		if !found {
			targets = append(slices.Clone(targets), messageFuzzTarget{unit: opts.unit, root: opts.root})
		}
	}
	roots := make([]*messageFuzzRoot, 0, len(targets))
	rootMap := make(map[string]int, len(targets))
	for _, t := range targets {
		key := t.unit + "." + t.root
		if _, exists := rootMap[key]; exists {
			continue
		}
		irUnit, err := u.get(t.unit)
		if err != nil {
			return nil, nil, err
		}
		model := tabletext.NewModel(irUnit)
		def := model.Lookup(t.root)
		if def == nil || model.IsVariable(t.root) {
			continue
		}
		ann := tablewire.Announce(irUnit)
		vocab := &tablewire.Vocabulary{}
		if err := vocab.AnnounceRead(ann, &tabletext.Report{}); err != nil {
			return nil, nil, fmt.Errorf("%s: AnnounceRead of own unit failed: %w", key, err)
		}
		r := &messageFuzzRoot{
			unit:       t.unit,
			root:       t.root,
			model:      model,
			def:        def,
			announce:   ann,
			vocabulary: vocab,
			entries:    tablewire.MessageVocabulary(irUnit),
		}
		rootMap[key] = len(roots)
		roots = append(roots, r)
	}
	return roots, rootMap, nil
}

// wrapVocabularyAnnouncement wraps raw vocabulary bytes into a syntactically
// valid Form-1 announcement file with the given buildVersion.
func wrapVocabularyAnnouncement(buildVersion uint64, vocabBytes []byte) []byte {
	out := make([]byte, 0, 32+len(vocabBytes)+24)
	out = append(out, ir.TableWireForm)
	out = append(out, 1)                      // ref 1: build version
	out = append(out, uint8(ir.TableKindU64)) // kind 9
	out = binary.LittleEndian.AppendUint64(out, buildVersion)
	out = append(out, 2, uint8(ir.TableKindArray)) // ref 2: vocabulary byte array
	body := make([]byte, 0, len(vocabBytes)+16)
	body = append(body, uint8(ir.TableKindU8))
	body = appendUleb(body, uint64(len(vocabBytes)))
	body = append(body, vocabBytes...)
	out = appendUleb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0) // end of root body
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

func appendUleb(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func buildMessageFuzzCorpus(
	u *units,
	m *Manifest,
	roots []*messageFuzzRoot,
	rootMap map[string]int,
	known []bool,
	jsonDir string,
	seed uint64,
) (systematic []messageFuzzCase, seedBatches []messageFuzzCase, err error) {
	rng := rand.New(rand.NewPCG(seed^0x243f6a8885a308d3, 0x13198a2e03707344))

	add := func(tc messageFuzzCase, isSeed bool) {
		systematic = append(systematic, tc)
		if isSeed && len(tc.batch) > 0 {
			seedBatches = append(seedBatches, tc)
		}
	}

	// 1. Existing MANIFEST.txt Messages & MessageDrifts.
	for _, msg := range m.Messages {
		if msg.Refused() {
			continue
		}
		conn, cerr := m.LookupConnection(msg.Connection)
		if cerr != nil {
			continue
		}
		ri, ok := rootMap[conn.Unit+"."+msg.Root]
		if !ok || !known[ri] {
			continue
		}
		wire, rerr := os.ReadFile(msg.MessageWire)
		if rerr != nil {
			return nil, nil, rerr
		}
		add(messageFuzzCase{name: "manifest-msg-" + msg.Name, rootIdx: ri, batch: wire}, true)
	}
	for _, md := range m.MessageDrifts {
		ri, ok := rootMap[md.Unit+"."+md.Root]
		if !ok || !known[ri] {
			continue
		}
		pc, cerr := m.LookupConnection(md.PeerConn)
		if cerr != nil {
			continue
		}
		ann, aerr := os.ReadFile(pc.Wire)
		if aerr != nil {
			return nil, nil, aerr
		}
		wire, werr := os.ReadFile(md.PeerWire)
		if werr != nil {
			return nil, nil, werr
		}
		add(messageFuzzCase{name: "manifest-drift-" + md.Name, rootIdx: ri, announce: ann, batch: wire}, true)
	}

	// Collect JSON seed instances per (unit, root) when available.
	jsonInstances := map[string][]*tabletext.Instance{}
	if jsonDir != "" {
		for _, inst := range m.Instances {
			if inst.NoText || inst.WireRefused {
				continue
			}
			ri, ok := rootMap[inst.Unit+"."+inst.Root]
			if !ok {
				continue
			}
			raw, rerr := os.ReadFile(filepath.Join(jsonDir, inst.Name+".json"))
			if rerr != nil || len(raw) > 16384 {
				continue
			}
			r := roots[ri]
			val := r.model.New(r.def)
			var rep tabletext.Report
			if r.model.Read(val, raw, &rep) && strictReportClean(&rep) && r.model.CheckBinaryValue(val) == nil {
				jsonInstances[inst.Unit+"."+inst.Root] = append(jsonInstances[inst.Unit+"."+inst.Root], val)
			}
		}
	}

	// 2. Per-root synthesized bodies, multi-body batches (0, 1, 2, 5, 8),
	// non-empty string/bytes default boundaries, and quantized float step boundaries.
	rootBodies := make([][]*tabletext.Instance, len(roots))
	for ri, r := range roots {
		bodies := synthesizeRootBodies(rng, r, jsonInstances[r.unit+"."+r.root])
		rootBodies[ri] = bodies
		if !known[ri] {
			continue
		}

		// 0-body & bad-form framing refusals
		for bi, bad := range [][]byte{
			{},
			{ir.TableWireMessageForm},
			{0x00, 0x00, 0x00},
			{ir.TableWireForm, 0x00, 0x00},
			{0x03, 0x00, 0x00},
			{0xff, 0x00, 0x00},
		} {
			add(messageFuzzCase{
				name:    fmt.Sprintf("zero-or-badform-%s.%s-%d", r.unit, r.root, bi),
				rootIdx: ri,
				batch:   bad,
			}, false)
		}

		// 1-body batches for every synthesized boundary instance
		for bi, inst := range bodies {
			enc, eerr := tablewire.EncodeMessages(r.model, []*tabletext.Instance{inst})
			if eerr == nil {
				add(messageFuzzCase{
					name:    fmt.Sprintf("synth1-%s.%s-%d", r.unit, r.root, bi),
					rootIdx: ri,
					batch:   enc,
				}, true)
			}
		}

		// Multi-body batches: 2, 5, and 8 bodies in one continuous bit stream!
		for _, count := range []int{2, 5, 8} {
			batchInsts := make([]*tabletext.Instance, count)
			for k := range count {
				batchInsts[k] = bodies[(k*3+count)%len(bodies)]
			}
			enc, eerr := tablewire.EncodeMessages(r.model, batchInsts)
			if eerr == nil {
				add(messageFuzzCase{
					name:    fmt.Sprintf("batch%d-%s.%s", count, r.unit, r.root),
					rootIdx: ri,
					batch:   enc,
				}, true)
			}
		}

		// Systematic announcement & vocabulary mutations on this root!
		sampleBatch, _ := tablewire.EncodeMessages(r.model, []*tabletext.Instance{bodies[min(1, len(bodies)-1)]})
		if len(sampleBatch) > 0 {
			for _, ac := range buildAnnouncementMutants(rng, r, bodies, sampleBatch) {
				ac.rootIdx = ri
				add(ac, ac.announce != nil && len(ac.batch) > 2)
			}
			// Systematic body bitstream mutations (tail bits, truncation, trailing garbage,
			// quantized flips, UTF-8/NUL string corruption, over-bound counts, non-finite floats).
			multi2 := sampleBatch
			if len(bodies) >= 2 {
				if b2, e2 := tablewire.EncodeMessages(r.model, []*tabletext.Instance{bodies[0], bodies[1]}); e2 == nil {
					multi2 = b2
				}
			}
			for _, bc := range buildBodyMutants(r, sampleBatch, multi2) {
				bc.rootIdx = ri
				add(bc, false)
			}
		}
	}

	// 3. Cross-version multi-body batches (1, 2, 5, 8 bodies): VarEvo1<->VarEvo3,
	// VarEvo2->VarEvo3, V1<->V2, M1<->M2, A1<->A2, K1<->K2, Scalars<->Scalars2!
	for _, cp := range messageCrossPairs {
		wIdx, wok := rootMap[cp.writerUnit+"."+cp.root]
		rIdx, rok := rootMap[cp.readerUnit+"."+cp.root]
		if !wok || !rok || !known[rIdx] {
			continue
		}
		writerRoot := roots[wIdx]
		wBodies := rootBodies[wIdx]
		if len(wBodies) == 0 {
			continue
		}
		for _, count := range []int{1, 2, 5, 8} {
			batchInsts := make([]*tabletext.Instance, count)
			for k := range count {
				batchInsts[k] = wBodies[(k+count-1)%len(wBodies)]
			}
			enc, eerr := tablewire.EncodeMessages(writerRoot.model, batchInsts)
			if eerr != nil {
				continue
			}
			tc := messageFuzzCase{
				name:     fmt.Sprintf("cross-%s-to-%s.%s-b%d", cp.writerUnit, cp.readerUnit, cp.root, count),
				rootIdx:  rIdx,
				announce: bytes.Clone(writerRoot.announce),
				batch:    enc,
			}
			add(tc, true)
			// Also flip bits in cross-version batch to exercise cross-version refusal rollback!
			if len(enc) > 2 {
				mut := bytes.Clone(enc)
				mut[len(mut)-1] ^= 0x80
				add(messageFuzzCase{
					name:     tc.name + "-tailflip",
					rootIdx:  rIdx,
					announce: bytes.Clone(writerRoot.announce),
					batch:    mut,
				}, false)
				add(messageFuzzCase{
					name:     tc.name + "-trunc",
					rootIdx:  rIdx,
					announce: bytes.Clone(writerRoot.announce),
					batch:    enc[:len(enc)-1],
				}, false)
			}
		}
	}

	return systematic, seedBatches, nil
}

// synthesizeRootBodies builds deterministic boundary instances for a fixed root:
//   - [0]: declared defaults
//   - [1..3]: rich populated random valid instances
//   - string/bytes default boundary instances ("" vs declared non-empty default vs modified)
//   - quantized float step boundary instances (default, min, max, default+-res, sub-step -> default)
func synthesizeRootBodies(rng *rand.Rand, r *messageFuzzRoot, fromJSON []*tabletext.Instance) []*tabletext.Instance {
	var out []*tabletext.Instance
	// [0]: all defaults
	out = append(out, r.model.New(r.def))
	out = append(out, fromJSON...)

	// 3 rich populated instances (varied counts, union arms, non-default scalars/strings)
	for mode := range 3 {
		inst := r.model.New(r.def)
		populateMessageInstance(rng, r.model, inst, mode, 0)
		if r.model.CheckBinaryValue(inst) == nil {
			out = append(out, inst)
		}
	}

	// Non-empty string/bytes default & quantized float boundary instances on top-level fields
	for fi, f := range r.def.Fields {
		if f.Array != ir.ArrayNone || f.KeyEnum != "" || f.MapEntry != nil {
			continue
		}
		if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && f.HasDefault && len(f.DefBytes) > 0 {
			// Empty "" when default is non-empty MUST NOT be elided!
			emptyInst := r.model.New(r.def)
			fv := &emptyInst.Fields[fi]
			if f.Type.Optional {
				fv.Present = true
			}
			fv.Cell.Str = []byte{}
			fv.Count = 0
			if r.model.CheckBinaryValue(emptyInst) == nil {
				out = append(out, emptyInst)
			}
			// Prefix of default string/bytes (same prefix, shorter length!)
			if len(f.DefBytes) > 1 {
				pref := bytes.Clone(f.DefBytes[:len(f.DefBytes)-1])
				for f.Type.Kind == ir.TString && len(pref) > 0 && (pref[len(pref)-1]&0xC0) == 0x80 {
					pref = pref[:len(pref)-1]
				}
				if f.Type.Kind == ir.TString && len(pref) > 0 && pref[len(pref)-1] >= 0x80 {
					pref = pref[:len(pref)-1]
				}
				prefInst := r.model.New(r.def)
				pfv := &prefInst.Fields[fi]
				if f.Type.Optional {
					pfv.Present = true
				}
				pfv.Cell.Str = pref
				pfv.Count = len(pref)
				if r.model.CheckBinaryValue(prefInst) == nil {
					out = append(out, prefInst)
				}
			}
		}
		if (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64) && f.HasFloatRange && f.Resolution > 0 {
			defVal := float64(0)
			if f.HasDefault {
				defVal = f.DefFloat
			} else if f.FMin > 0 || f.FMax < 0 {
				defVal = f.FMin
			}
			candidates := []float64{
				f.FMin,
				f.FMax,
				defVal + f.Resolution,
				defVal - f.Resolution,
				defVal + 0.1*f.Resolution, // quantizes to declared default step!
				defVal - 0.1*f.Resolution, // quantizes to declared default step!
			}
			for _, cand := range candidates {
				if cand < f.FMin || cand > f.FMax {
					continue
				}
				if f.Type.Kind == ir.TFloat32 {
					cand = float64(float32(cand))
					if cand < f.FMin || cand > f.FMax {
						continue
					}
				}
				qInst := r.model.New(r.def)
				qfv := &qInst.Fields[fi]
				if f.Type.Optional {
					qfv.Present = true
				}
				qfv.Cell.F = cand
				if r.model.CheckBinaryValue(qInst) == nil {
					out = append(out, qInst)
				}
			}
		}
	}
	return out
}

func populateMessageInstance(rng *rand.Rand, model *tabletext.Model, inst *tabletext.Instance, mode, depth int) {
	if depth > 5 || inst == nil {
		return
	}
	for fi := range inst.Fields {
		fv := &inst.Fields[fi]
		f := fv.Def
		if f.Type.Optional {
			fv.Present = (mode+fi)%2 == 0
			if !fv.Present {
				continue
			}
		}
		switch {
		case f.KeyEnum != "":
			for idx := range fv.Elems {
				populateMessageCell(rng, model, f, &fv.Elems[idx], mode+idx, depth+1)
			}
		case f.Array == ir.ArrayFixed:
			for idx := range fv.Elems {
				populateMessageCell(rng, model, f, &fv.Elems[idx], mode+idx, depth+1)
			}
		case f.Array == ir.ArrayCounted:
			lo := int(f.ArrayMin)
			hi := int(f.ArrayBound)
			var cnt int
			switch mode % 3 {
			case 0:
				cnt = lo
			case 1:
				cnt = min(hi, max(lo, 1+(rng.IntN(max(1, min(4, hi-lo+1))))))
			default:
				cnt = min(hi, max(lo, min(hi, 4)))
			}
			fv.Count = cnt
			for idx := range cnt {
				populateMessageCell(rng, model, f, &fv.Elems[idx], mode+idx, depth+1)
			}
		default:
			populateMessageCell(rng, model, f, &fv.Cell, mode+fi, depth+1)
			if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
				fv.Count = len(fv.Cell.Str)
			}
		}
	}
}

func populateMessageCell(rng *rand.Rand, model *tabletext.Model, f *ir.Field, cell *tabletext.Cell, mode, depth int) {
	switch f.Type.Kind {
	case ir.TBool:
		cell.B = mode%2 == 0
	case ir.TInt:
		populateIntCell(rng, f, cell, mode)
	case ir.TBits:
		w := f.Type.Width
		mask := ^uint64(0)
		if w < 64 {
			mask = (uint64(1) << w) - 1
		}
		vals := []uint64{0, 1, mask, (mask >> 1) | 1}
		cell.U = vals[mode%len(vals)] & mask
	case ir.TFixed:
		populateFixedPointCell(f, cell, mode)
	case ir.TFloat32:
		if f.HasFloatRange {
			lo, hi := f.FMin, f.FMax
			if f.Resolution > 0 {
				steps := int(math.Floor((hi - lo) / f.Resolution))
				step := 0
				if steps > 0 {
					step = (mode * 3) % (steps + 1)
				}
				v := float64(float32(lo + float64(step)*f.Resolution))
				if v >= lo && v <= hi {
					cell.F = v
				}
			} else {
				cell.F = float64(float32(lo + (hi-lo)*0.25*float64(mode%5)))
			}
		} else {
			vals := []float32{0, -0.5, 1.25, -42.5, 1024.75}
			cell.F = float64(vals[mode%len(vals)])
		}
	case ir.TFloat64:
		if f.HasFloatRange {
			lo, hi := f.FMin, f.FMax
			if f.Resolution > 0 {
				steps := int(math.Floor((hi - lo) / f.Resolution))
				step := 0
				if steps > 0 {
					step = (mode * 5) % (steps + 1)
				}
				v := lo + float64(step)*f.Resolution
				if v >= lo && v <= hi {
					cell.F = v
				}
			} else {
				cell.F = lo + (hi-lo)*0.25*float64(mode%5)
			}
		} else {
			vals := []float64{0, -0.125, 3.5, -9007199254740992.0, 123456.789}
			cell.F = vals[mode%len(vals)]
		}
	case ir.TString:
		capBytes := int(f.Type.Size)
		if capBytes <= 0 || capBytes > 32 {
			capBytes = 16
		}
		samples := []string{"", "a", "hello", "quote\"ok", "é€", "🚀", "alpha-beta"}
		pick := samples[mode%len(samples)]
		for len(pick) > capBytes {
			pick = pick[:len(pick)-1]
			for len(pick) > 0 && (pick[len(pick)-1]&0xC0) == 0x80 {
				pick = pick[:len(pick)-1]
			}
			if len(pick) > 0 && pick[len(pick)-1] >= 0x80 {
				pick = pick[:len(pick)-1]
			}
		}
		cell.Str = []byte(pick)
	case ir.TBytes:
		capBytes := int(f.Type.Size)
		if capBytes <= 0 || capBytes > 32 {
			capBytes = 16
		}
		samples := [][]byte{{}, {0x00}, {0xde, 0xad, 0xbe, 0xef}, {0x01, 0x02, 0x03, 0xff, 0x00}}
		b := samples[mode%len(samples)]
		if len(b) > capBytes {
			b = b[:capBytes]
		}
		cell.Str = bytes.Clone(b)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			if len(ref.Variants) > 0 {
				cell.U = uint64((mode % len(ref.Variants)) + 1)
			}
		case *ir.Flags:
			if len(ref.Variants) > 0 {
				var mask uint64
				for bi := range ref.Variants {
					if (mode+bi)%2 == 0 {
						mask |= uint64(1) << bi
					}
				}
				cell.U = mask
			}
		case *ir.Struct:
			if cell.Tab == nil {
				cell.Tab = model.New(ref)
			}
			populateMessageInstance(rng, model, cell.Tab, mode+1, depth+1)
		case *ir.Union:
			total := len(ref.Variants) + 1
			tag := mode % total
			cell.U = uint64(tag)
			cell.Tab = nil
			cell.Arm = nil
			if tag > 0 {
				v := ref.Variants[tag-1]
				if v.F != nil {
					if st, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed && v.F.Array == ir.ArrayNone && v.F.KeyEnum == "" {
						cell.Tab = model.New(st)
						populateMessageInstance(rng, model, cell.Tab, mode+1, depth+1)
					} else {
						tmp := &ir.Struct{Name: "_arm", Fields: []*ir.Field{v.F}}
						armInst := model.New(tmp)
						populateMessageInstance(rng, model, armInst, mode+1, depth+1)
						af := armInst.Fields[0]
						cell.Arm = &af
					}
				}
			}
		}
	}
}

func populateIntCell(rng *rand.Rand, f *ir.Field, cell *tabletext.Cell, mode int) {
	if f.Type.Timestamp {
		vals := []int64{0, 1700000000000000000, -1000000000}
		cell.I = vals[mode%len(vals)]
		return
	}
	if f.Type.Width == 128 {
		if f.HasIntRange {
			if mode%2 == 0 {
				cell.Wide = new(big.Int).Set(f.IntMin)
			} else {
				cell.Wide = new(big.Int).Set(f.IntMax)
			}
			return
		}
		if f.Type.Signed {
			vals := []*big.Int{
				big.NewInt(0),
				big.NewInt(-1),
				new(big.Int).Lsh(big.NewInt(1), 70),
				new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 70)),
			}
			cell.Wide = new(big.Int).Set(vals[mode%len(vals)])
		} else {
			vals := []*big.Int{
				big.NewInt(0),
				big.NewInt(42),
				new(big.Int).Lsh(big.NewInt(1), 72),
			}
			cell.Wide = new(big.Int).Set(vals[mode%len(vals)])
		}
		return
	}
	if f.Type.Signed {
		lo := -(int64(1) << (f.Type.Width - 1))
		hi := (int64(1) << (f.Type.Width - 1)) - 1
		if f.Type.Width == 64 {
			lo, hi = math.MinInt64, math.MaxInt64
		}
		if f.HasIntRange {
			lo, hi = f.IntMin.Int64(), f.IntMax.Int64()
		}
		mid := lo + (hi-lo)/2
		vals := []int64{lo, hi, mid}
		if 0 >= lo && 0 <= hi {
			vals = append(vals, 0)
		}
		cell.I = vals[mode%len(vals)]
	} else {
		lo := uint64(0)
		hi := ^uint64(0)
		if f.Type.Width < 64 {
			hi = (uint64(1) << f.Type.Width) - 1
		}
		if f.HasIntRange {
			lo, hi = f.IntMin.Uint64(), f.IntMax.Uint64()
		}
		mid := lo + (hi-lo)/2
		vals := []uint64{lo, hi, mid}
		cell.U = vals[mode%len(vals)]
	}
	_ = rng
}

func populateFixedPointCell(f *ir.Field, cell *tabletext.Cell, mode int) {
	if f.HasIntRange {
		if mode%2 == 0 {
			cell.Wide = new(big.Int).Set(f.IntMin)
		} else {
			cell.Wide = new(big.Int).Set(f.IntMax)
		}
		return
	}
	if f.Type.Signed {
		vals := []*big.Int{big.NewInt(0), big.NewInt(1), big.NewInt(-1)}
		cell.Wide = new(big.Int).Set(vals[mode%len(vals)])
	} else {
		vals := []*big.Int{big.NewInt(0), big.NewInt(1), big.NewInt(2)}
		cell.Wide = new(big.Int).Set(vals[mode%len(vals)])
	}
}

func buildAnnouncementMutants(
	rng *rand.Rand,
	r *messageFuzzRoot,
	bodies []*tabletext.Instance,
	sampleBatch []byte,
) []messageFuzzCase {
	var out []messageFuzzCase
	prefix := "ann-" + r.unit + "." + r.root + "-"

	// 1. Raw announcement framing damage: empty, wrong form, truncated, trailing garbage.
	for i, badAnn := range [][]byte{
		{0x00}, // non-empty so unpackMessageFuzzFrame routes to custom AnnounceRead!
		{ir.TableWireMessageForm, 0x00, 0x00},
		{0x03, 0x00, 0x00},
		r.announce[:min(5, len(r.announce))],
		r.announce[:max(1, len(r.announce)/2)],
		r.announce[:max(1, len(r.announce)-1)],
		append(bytes.Clone(r.announce), 0x00),
		append(bytes.Clone(r.announce), 0xff, 0x01),
	} {
		out = append(out, messageFuzzCase{
			name:     fmt.Sprintf("%sraw-%d", prefix, i),
			announce: badAnn,
			batch:    sampleBatch,
		})
	}

	// 2. GrowIdTable: 64, 65, 4096 (lawful cap), 4097 (refused cap), and duplicate IDs.
	idCount := int(binary.LittleEndian.Uint64(r.announce[len(r.announce)-8:]))
	for _, targetIDs := range []int{max(idCount+1, 64), max(idCount+2, 65), 4096, 4097} {
		if grown, err := GrowIdTable(r.announce, targetIDs, false); err == nil {
			out = append(out, messageFuzzCase{
				name:     fmt.Sprintf("%sgrown-%d", prefix, targetIDs),
				announce: grown,
				batch:    sampleBatch,
			})
		}
	}
	if dupIDs, err := GrowIdTable(r.announce, max(idCount+1, 64), true); err == nil {
		out = append(out, messageFuzzCase{
			name:     prefix + "grown-dup-id",
			announce: dupIDs,
			batch:    sampleBatch,
		})
	}

	peerVersion := r.vocabulary.BuildVersion() ^ 0x55aa55aa55aa55aa

	// 3. Empty vocabulary (0 entries): valid on an all-defaults batch, refuses nonzero refs!
	emptyAnn := wrapVocabularyAnnouncement(peerVersion, nil)
	if defBatch, err := tablewire.EncodeMessagesWithVocabulary(r.model, []*tabletext.Instance{r.model.New(r.def)}, nil); err == nil {
		out = append(out, messageFuzzCase{
			name:     prefix + "vocab-0-default-batch",
			announce: emptyAnn,
			batch:    defBatch,
		})
	}
	out = append(out, messageFuzzCase{
		name:     prefix + "vocab-0-nonempty-batch",
		announce: emptyAnn,
		batch:    sampleBatch,
	})

	// 4. 65-entry & 130-entry vocabulary (breaks any 64-entry announcement cap!):
	// Prepend unknown lawful uint8 entries so real fields sit past slot 64 (refBits >= 7)!
	for _, totalEntries := range []int{65, 130} {
		if len(r.entries) < totalEntries {
			padCount := totalEntries - len(r.entries)
			padded := make([]ir.TableVocabularyEntry, 0, totalEntries)
			for k := range padCount {
				padded = append(padded, ir.TableVocabularyEntry{
					Id:   0x9e3779b97f4a7c15 ^ uint64(k+1)*0xbf58476d1ce4e5b9,
					Kind: uint8(ir.TableKindU8),
				})
			}
			padded = append(padded, r.entries...)
			ann65 := wrapVocabularyAnnouncement(peerVersion^uint64(totalEntries), ir.TableVocabularyBytes(padded))
			pick := []*tabletext.Instance{bodies[min(1, len(bodies)-1)]}
			if len(bodies) >= 2 {
				pick = []*tabletext.Instance{bodies[0], bodies[1]}
			}
			if enc65, err := tablewire.EncodeMessagesWithVocabulary(r.model, pick, padded); err == nil {
				out = append(out, messageFuzzCase{
					name:     fmt.Sprintf("%svocab-%d-shifted-slots", prefix, totalEntries),
					announce: ann65,
					batch:    enc65,
				})
			}
		}
	}

	// 5. Shuffled / out-of-order vocabulary entries under peer buildVersion!
	if len(r.entries) >= 2 {
		shuffled := slices.Clone(r.entries)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		shufAnn := wrapVocabularyAnnouncement(peerVersion^0x1234, ir.TableVocabularyBytes(shuffled))
		pick := []*tabletext.Instance{bodies[min(1, len(bodies)-1)]}
		if len(bodies) >= 3 {
			pick = []*tabletext.Instance{bodies[1], bodies[2]}
		}
		if shufBatch, err := tablewire.EncodeMessagesWithVocabulary(r.model, pick, shuffled); err == nil {
			out = append(out, messageFuzzCase{
				name:     prefix + "vocab-shuffled-positive",
				announce: shufAnn,
				batch:    shufBatch,
			})
		}
	}

	// 6. Duplicate vocabulary entry (must refuse!)
	if len(r.entries) >= 1 {
		dup := append(slices.Clone(r.entries), r.entries[0])
		out = append(out, messageFuzzCase{
			name:     prefix + "vocab-duplicate-entry",
			announce: wrapVocabularyAnnouncement(peerVersion^0x2222, ir.TableVocabularyBytes(dup)),
			batch:    sampleBatch,
		})
	}

	// 7. Reserved IDs & invalid kinds (0, 31, 34, 255) & invalid array/keyed elemKinds (0, 14, 31, 255)
	vocabBytes := ir.TableVocabularyBytes(r.entries)
	if len(vocabBytes) > 2 {
		out = append(out, messageFuzzCase{
			name:     prefix + "vocab-truncated-blob",
			announce: wrapVocabularyAnnouncement(peerVersion^0x3333, vocabBytes[:len(vocabBytes)-1]),
			batch:    sampleBatch,
		})
	}
	for _, badKind := range []byte{0, 31, 33, 34, 255} {
		var raw []byte
		raw = binary.LittleEndian.AppendUint64(raw, 0x1122334455667788)
		raw = append(raw, badKind)
		out = append(out, messageFuzzCase{
			name:     fmt.Sprintf("%svocab-bad-kind-%d", prefix, badKind),
			announce: wrapVocabularyAnnouncement(peerVersion^uint64(badKind), raw),
			batch:    sampleBatch,
		})
	}
	for _, badElem := range []byte{0, 14, 16, 31, 255} {
		// Kind 14 (array): elemKind byte, min leb, max leb, shape...
		var raw []byte
		raw = binary.LittleEndian.AppendUint64(raw, 0x2233445566778899)
		raw = append(raw, uint8(ir.TableKindArray), badElem, 0, 4)
		out = append(out, messageFuzzCase{
			name:     fmt.Sprintf("%svocab-bad-array-elem-%d", prefix, badElem),
			announce: wrapVocabularyAnnouncement(peerVersion^0x4400^uint64(badElem), raw),
			batch:    sampleBatch,
		})
		// Kind 16 (keyed): elemKind byte
		var kraw []byte
		kraw = binary.LittleEndian.AppendUint64(kraw, 0x33445566778899aa)
		kraw = append(kraw, uint8(ir.TableKindKeyed), badElem)
		out = append(out, messageFuzzCase{
			name:     fmt.Sprintf("%svocab-bad-keyed-elem-%d", prefix, badElem),
			announce: wrapVocabularyAnnouncement(peerVersion^0x5500^uint64(badElem), kraw),
			batch:    sampleBatch,
		})
	}

	// Hostile bit-width (> 128) and array min > max in vocabulary shape
	{
		var overBits []byte
		overBits = binary.LittleEndian.AppendUint64(overBits, 0x445566778899aabb)
		overBits = append(overBits, uint8(ir.TableKindU32), 1, 129, 0, 10) // ranged=1, bits=129
		out = append(out, messageFuzzCase{
			name:     prefix + "vocab-bits-129",
			announce: wrapVocabularyAnnouncement(peerVersion^0x6601, overBits),
			batch:    sampleBatch,
		})

		var minOverMax []byte
		minOverMax = binary.LittleEndian.AppendUint64(minOverMax, 0x5566778899aabbcc)
		minOverMax = append(minOverMax, uint8(ir.TableKindArray), uint8(ir.TableKindU8), 5, 2, 0) // min=5 > max=2
		out = append(out, messageFuzzCase{
			name:     prefix + "vocab-array-min-gt-max",
			announce: wrapVocabularyAnnouncement(peerVersion^0x6602, minOverMax),
			batch:    sampleBatch,
		})
	}

	// 8. Kind mismatch on a known field ID in peer announcement!
	for _, e := range r.entries {
		if e.Kind != 32 && e.Kind != uint8(ir.TableKindString) {
			mutEntries := slices.Clone(r.entries)
			for mi := range mutEntries {
				if mutEntries[mi].Id == e.Id && mutEntries[mi].Kind == e.Kind {
					mutEntries[mi] = ir.TableVocabularyEntry{
						Id:    e.Id,
						Kind:  uint8(ir.TableKindString),
						Shape: ir.TableMessageShape{Max: 8},
					}
					break
				}
			}
			out = append(out, messageFuzzCase{
				name:     prefix + "vocab-field-kind-mismatch",
				announce: wrapVocabularyAnnouncement(peerVersion^0x7777, ir.TableVocabularyBytes(mutEntries)),
				batch:    sampleBatch,
			})
			break
		}
	}

	return out
}

func buildBodyMutants(r *messageFuzzRoot, batch1, batch2 []byte) []messageFuzzCase {
	var out []messageFuzzCase
	prefix := "body-" + r.unit + "." + r.root + "-"

	// 1. Non-zero padding/tail bits in the final byte of 1-body & 2-body batches!
	for label, src := range map[string][]byte{"b1": batch1, "b2": batch2} {
		if len(src) == 0 {
			continue
		}
		for bit := range 8 {
			mut := bytes.Clone(src)
			mut[len(mut)-1] ^= byte(1 << bit)
			out = append(out, messageFuzzCase{
				name:  fmt.Sprintf("%s%s-lastbyte-bit%d", prefix, label, bit),
				batch: mut,
			})
		}
		// Truncations
		for cut := 1; cut < len(src) && cut <= 8; cut++ {
			out = append(out, messageFuzzCase{
				name:  fmt.Sprintf("%s%s-trunc-%d", prefix, label, cut),
				batch: bytes.Clone(src[:len(src)-cut]),
			})
		}
		// Trailing garbage bytes (zero and non-zero)
		for gi, tail := range [][]byte{{0x00}, {0x01}, {0xff}, {0x00, 0x00}, {0xaa, 0x55, 0x01, 0x02}} {
			out = append(out, messageFuzzCase{
				name:  fmt.Sprintf("%s%s-trail-%d", prefix, label, gi),
				batch: append(bytes.Clone(src), tail...),
			})
		}
		// Inflated count byte (claiming more bodies than stream holds)
		if len(src) >= 2 {
			for _, cByte := range []byte{1, 7, 31, 255} {
				mut := bytes.Clone(src)
				mut[1] = cByte
				out = append(out, messageFuzzCase{
					name:  fmt.Sprintf("%s%s-count-%d", prefix, label, int(cByte)+1),
					batch: mut,
				})
			}
		}
	}

	// 2. BitSpots-guided mutations (quantized fields, counts, UTF-8/NUL strings, non-finite floats)
	spots := tablewire.MessageSpots(r.model, r.def, batch1, r.vocabulary)
	for si, sp := range spots {
		if si >= 24 {
			break
		}
		bitBase := 8 + sp.Off // data[1:] bit offset -> batch byte 0 + 8 bits
		byteOff := bitBase / 8
		bitInByte := bitBase % 8
		if byteOff >= 0 && byteOff < len(batch1) {
			mut := bytes.Clone(batch1)
			mut[byteOff] ^= byte(1 << bitInByte)
			out = append(out, messageFuzzCase{
				name:  fmt.Sprintf("%sspot-%d-fliplo", prefix, si),
				batch: mut,
			})
			if sp.Width > 1 {
				hiBit := bitBase + sp.Width - 1
				if hiBit/8 < len(batch1) {
					mutHi := bytes.Clone(batch1)
					mutHi[hiBit/8] ^= byte(1 << (hiBit % 8))
					out = append(out, messageFuzzCase{
						name:  fmt.Sprintf("%sspot-%d-fliphi", prefix, si),
						batch: mutHi,
					})
				}
				// Set all bits in spot to 1 (triggers out-of-range vocabulary slot E+1 / max)
				mutAll := bytes.Clone(batch1)
				for b := range sp.Width {
					pos := bitBase + b
					if pos/8 < len(mutAll) {
						mutAll[pos/8] |= byte(1 << (pos % 8))
					}
				}
				out = append(out, messageFuzzCase{
					name:  fmt.Sprintf("%sspot-%d-allones", prefix, si),
					batch: mutAll,
				})
			}
		}
	}

	// 3. Explicit UTF-8 / NUL & non-finite float payload injections across byte positions!
	if len(batch1) >= 6 {
		badPayloads := [][]byte{
			{0x00},                   // embedded NUL in string
			{0x80},                   // lone UTF-8 continuation
			{0xc0, 0x80},             // overlong NUL
			{0xed, 0xa0, 0x80},       // UTF-16 surrogate U+D800
			{0xff},                   // invalid 0xFF UTF-8 byte
			{0x00, 0x00, 0x80, 0x7f}, // float32 +Inf LE
			{0x00, 0x00, 0x80, 0xff}, // float32 -Inf LE
			{0x01, 0x00, 0x80, 0x7f}, // float32 signaling NaN LE
			{0x00, 0x00, 0xc0, 0x7f}, // float32 quiet NaN LE
			{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x7f}, // float64 +Inf LE
			{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x7f}, // float64 signaling NaN LE
		}
		step := max(1, (len(batch1)-2)/4)
		for off := 2; off < len(batch1); off += step {
			for pi, pat := range badPayloads {
				if off+len(pat) > len(batch1) {
					continue
				}
				mut := bytes.Clone(batch1)
				copy(mut[off:], pat)
				out = append(out, messageFuzzCase{
					name:  fmt.Sprintf("%sinject-off%d-p%d", prefix, off, pi),
					batch: mut,
				})
			}
		}
	}
	return out
}

func randomMessageMutant(
	rng *rand.Rand,
	roots []*messageFuzzRoot,
	known []bool,
	seeds []messageFuzzCase,
	iter int,
) messageFuzzCase {
	if len(seeds) == 0 {
		for ri := range roots {
			if known[ri] {
				return messageFuzzCase{name: fmt.Sprintf("rand-%d", iter), rootIdx: ri, batch: []byte{ir.TableWireMessageForm, 0, 0}}
			}
		}
	}
	base := seeds[rng.IntN(len(seeds))]
	ann := bytes.Clone(base.announce)
	batch := bytes.Clone(base.batch)

	switch rng.IntN(8) {
	case 0, 1, 2:
		// 1..4 random bit flips in batch (preserving form byte 80% of the time to reach deep bit decoders)
		if len(batch) > 0 {
			flips := 1 + rng.IntN(4)
			for range flips {
				start := 0
				if len(batch) > 1 && rng.IntN(5) != 0 {
					start = 1
				}
				idx := start + rng.IntN(len(batch)-start)
				batch[idx] ^= byte(1 << rng.IntN(8))
			}
		}
	case 3:
		// Truncate or extend batch
		if len(batch) > 2 && rng.IntN(2) == 0 {
			batch = batch[:1+rng.IntN(len(batch)-1)]
		} else {
			extra := 1 + rng.IntN(8)
			for range extra {
				batch = append(batch, byte(rng.UintN(256)))
			}
		}
	case 4:
		// Overwrite a slice with boundary bytes (0x00, 0xFF, NaN/Inf, invalid UTF-8)
		if len(batch) > 2 {
			pos := 1 + rng.IntN(len(batch)-1)
			tokens := [][]byte{
				{0xff, 0xff},
				{0x00, 0x00},
				{0x01, 0x00, 0x80, 0x7f},
				{0xed, 0xa0, 0x80},
				{0x80, 0xff},
			}
			tok := tokens[rng.IntN(len(tokens))]
			end := min(len(batch), pos+len(tok))
			copy(batch[pos:end], tok[:end-pos])
		}
	case 5:
		// Concatenate / splice two batches of the same root into a multi-body mutant
		var sameRoot []messageFuzzCase
		for _, s := range seeds {
			if s.rootIdx == base.rootIdx && len(s.announce) == len(base.announce) {
				sameRoot = append(sameRoot, s)
			}
		}
		if len(sameRoot) >= 2 && len(batch) >= 2 {
			other := sameRoot[rng.IntN(len(sameRoot))]
			if len(other.batch) > 2 {
				batch[1] = byte(min(255, int(batch[1])+int(other.batch[1])+1))
				batch = append(batch, other.batch[2:]...)
			}
		}
	case 6:
		// Mutate peer announcement bytes (or promote same-build to peer announcement & mutate!)
		if len(ann) == 0 {
			ann = bytes.Clone(roots[base.rootIdx].announce)
		}
		if len(ann) > 2 {
			if rng.IntN(3) == 0 {
				ann = ann[:1+rng.IntN(len(ann)-1)]
			} else {
				flips := 1 + rng.IntN(3)
				for range flips {
					idx := 1 + rng.IntN(len(ann)-1)
					ann[idx] ^= byte(1 << rng.IntN(8))
				}
			}
		}
	default:
		// Synthesize fresh random 1..4-body valid batch, then optionally flip 1 bit 50% of the time!
		r := roots[base.rootIdx]
		cnt := 1 + rng.IntN(4)
		insts := make([]*tabletext.Instance, cnt)
		for i := range cnt {
			inst := r.model.New(r.def)
			populateMessageInstance(rng, r.model, inst, rng.IntN(16), 0)
			if r.model.CheckBinaryValue(inst) != nil {
				inst = r.model.New(r.def)
			}
			insts[i] = inst
		}
		if enc, err := tablewire.EncodeMessages(r.model, insts); err == nil {
			ann = nil
			batch = enc
			if rng.IntN(2) == 0 && len(batch) > 2 {
				pos := 2 + rng.IntN(len(batch)-2)
				batch[pos] ^= byte(1 << rng.IntN(8))
			}
		}
	}

	return messageFuzzCase{
		name:     fmt.Sprintf("rand-%d", iter),
		rootIdx:  base.rootIdx,
		announce: ann,
		batch:    batch,
	}
}

// Keep unused imports referenced cleanly if needed by future extensions.
var (
	_ = json.Valid
	_ = sort.Strings
	_ = strconv.Itoa
)
