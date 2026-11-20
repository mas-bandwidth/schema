package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type jsonFuzzOptions struct {
	driversPath string
	driverCmd   string
	only        string
	skip        string
	work        string
	failed      string
	seed        uint64
	n           int
	jsonDir     string
}

type jsonFuzzTarget struct {
	unit string
	root string
}

// jsonFuzzTargets names the 10 (unit, root) pairs carried across BOTH
// `json-hostile` and `json-write` by all 18 conformance drivers, spanning
// variable tables, fixed tables (form 3), `json = "array"` roots, maps,
// unbounded text lists, `total = N` text lists, `*bytes` / `*string` blobs,
// pointer graphs, general unions, `fixed`/`ufixed`, `int128`/`uint128`,
// `timestamp`, `flags`, `enum`, `bits(N)`, ranged scalars, and `required`.
var jsonFuzzTargets = []jsonFuzzTarget{
	{unit: "featurewitnesses", root: "Root"},
	{unit: "featurewitnesses", root: "ArrayRoot"},
	{unit: "tabledemo", root: "RootConfig"},
	{unit: "scalars", root: "SimState"},
	{unit: "blobdemo", root: "Catalog"},
	{unit: "graphdemo", root: "Scene"},
	{unit: "listdemo", root: "Army"},
	{unit: "mapdemo", root: "Text"},
	{unit: "messagedemo", root: "ToolMessage"},
	{unit: "streamdemo", root: "Feed"},
}

type jsonFuzzSeedDoc struct {
	name string
	unit string
	root string
	raw  []byte
}

type jsonFuzzRawCase struct {
	category string
	unit     string
	root     string
	raw      []byte
}

type jsonFuzzEvaluatedCase struct {
	name      string
	category  string
	unit      string
	root      string
	raw       []byte
	accepted  bool
	canonJSON []byte
	canonWire []byte
	treeRel   string
	wireRel   string
}

const maxJSONFuzzBatch = 1000

func jsonFuzz(w io.Writer, m *Manifest, opts jsonFuzzOptions) error {
	n := opts.n
	if n <= 0 {
		n = 360
	}
	drivers, err := resolveJSONFuzzDrivers(opts)
	if err != nil {
		return fmt.Errorf("json-fuzz: %w", err)
	}
	if len(drivers) == 0 {
		return errors.New("json-fuzz: no matching drivers")
	}

	u := newUnits(m)
	rawCases, err := generateJSONFuzzRawCases(m, opts.jsonDir, opts.seed, n)
	if err != nil {
		return fmt.Errorf("json-fuzz: %w", err)
	}

	workRoot, cleanWork, err := prepareJSONFuzzWork(opts.work)
	if err != nil {
		return fmt.Errorf("json-fuzz: %w", err)
	}
	if cleanWork {
		defer func() { _ = os.RemoveAll(workRoot) }()
	}

	type batchData struct {
		dir         string
		manifestRel string
		cases       []jsonFuzzEvaluatedCase
		accepted    int
	}
	var batches []batchData
	totalAccepted := 0
	for start := 0; start < len(rawCases); start += maxJSONFuzzBatch {
		end := min(len(rawCases), start+maxJSONFuzzBatch)
		batchDir := workRoot
		if len(rawCases) > maxJSONFuzzBatch {
			batchDir = filepath.Join(workRoot, fmt.Sprintf("b%03d", start/maxJSONFuzzBatch))
			if err := os.MkdirAll(batchDir, 0o755); err != nil {
				return fmt.Errorf("json-fuzz: %w", err)
			}
		}
		cases, acc, merr := materializeJSONFuzzCorpus(u, m, batchDir, rawCases[start:end], start)
		if merr != nil {
			return fmt.Errorf("json-fuzz: %w", merr)
		}
		totalAccepted += acc
		batches = append(batches, batchData{
			dir:         batchDir,
			manifestRel: repoRelative(filepath.Join(batchDir, "manifest.txt")),
			cases:       cases,
			accepted:    acc,
		})
	}

	var allFailures []string
	for _, d := range drivers {
		var failures []string
		for _, b := range batches {
			bf, rerr := runJSONFuzzDriver(d, b.manifestRel, b.dir, b.cases, opts.failed)
			if rerr != nil {
				return fmt.Errorf("json-fuzz (%s): %w", d.lang, rerr)
			}
			failures = append(failures, bf...)
			if len(failures) > 0 {
				break
			}
		}
		if len(failures) == 0 {
			_, _ = fmt.Fprintf(w, "json-fuzz %-10s seed=%d cases=%d (accepted=%d refused=%d): PASS\n",
				d.lang, opts.seed, len(rawCases), totalAccepted, len(rawCases)-totalAccepted)
			continue
		}
		_, _ = fmt.Fprintf(w, "json-fuzz %-10s seed=%d cases=%d: FAIL (%d divergences)\n",
			d.lang, opts.seed, len(rawCases), len(failures))
		for _, f := range failures {
			_, _ = fmt.Fprintf(w, "  %s\n", f)
			allFailures = append(allFailures, d.lang+": "+f)
		}
	}
	if len(allFailures) > 0 {
		return fmt.Errorf("json-fuzz: %d divergence(s) across %d driver(s)", len(allFailures), len(drivers))
	}
	return nil
}

func resolveJSONFuzzDrivers(opts jsonFuzzOptions) ([]driver, error) {
	if opts.driverCmd != "" {
		fields := strings.Fields(opts.driverCmd)
		if len(fields) == 0 {
			return nil, errors.New("empty --driver command")
		}
		lang := opts.only
		if lang == "" {
			lang = "custom"
		}
		return []driver{{lang: lang, argv: fields}}, nil
	}
	all, _, err := loadDrivers(opts.driversPath)
	if err != nil {
		return nil, err
	}
	skipped := map[string]bool{}
	for tok := range strings.FieldsFuncSeq(opts.skip, func(r rune) bool { return r == ',' || r == ' ' }) {
		if tok != "" {
			skipped[tok] = true
		}
	}
	onlySet := map[string]bool{}
	if opts.only != "" {
		for tok := range strings.FieldsFuncSeq(opts.only, func(r rune) bool { return r == ',' || r == ' ' }) {
			if tok != "" {
				onlySet[tok] = true
			}
		}
	}
	var out []driver
	for _, d := range all {
		if len(onlySet) > 0 && !onlySet[d.lang] {
			continue
		}
		if skipped[d.lang] {
			continue
		}
		out = append(out, d)
	}
	if len(onlySet) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("unknown language %q", opts.only)
	}
	return out, nil
}

func prepareJSONFuzzWork(work string) (string, bool, error) {
	if err := os.MkdirAll("build", 0o755); err != nil {
		return "", false, err
	}
	if work == "" || work == defaultWork {
		tmp, err := os.MkdirTemp("build", "json-fuzz-")
		if err != nil {
			return "", false, err
		}
		return repoRelative(tmp), true, nil
	}
	rel := repoRelative(work)
	if err := os.RemoveAll(rel); err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(rel, 0o755); err != nil {
		return "", false, err
	}
	return rel, false, nil
}

func materializeJSONFuzzCorpus(u *units, m *Manifest, workDir string, rawCases []jsonFuzzRawCase, baseIdx int) ([]jsonFuzzEvaluatedCase, int, error) {
	models := map[string]*tabletext.Model{}
	neededUnits := map[string]bool{}
	for _, t := range jsonFuzzTargets {
		neededUnits[t.unit] = true
		irUnit, err := u.get(t.unit)
		if err != nil {
			return nil, 0, err
		}
		models[t.unit] = tabletext.NewModel(irUnit)
	}

	treesDir := filepath.Join(workDir, "trees")
	wiresDir := filepath.Join(workDir, "wires")
	if err := os.MkdirAll(treesDir, 0o755); err != nil {
		return nil, 0, err
	}
	if err := os.MkdirAll(wiresDir, 0o755); err != nil {
		return nil, 0, err
	}

	cases := make([]jsonFuzzEvaluatedCase, 0, len(rawCases))
	acceptedCount := 0
	for idx, rc := range rawCases {
		model := models[rc.unit]
		if model == nil {
			return nil, 0, fmt.Errorf("unregistered unit %q", rc.unit)
		}
		st := model.Lookup(rc.root)
		if st == nil {
			return nil, 0, fmt.Errorf("unit %s has no root %s", rc.unit, rc.root)
		}
		val := model.New(st)
		var report tabletext.Report
		ok := strictReadJSON(model, val, rc.raw, &report)

		name := fmt.Sprintf("jf%05d", baseIdx+idx)
		treePath := filepath.Join(treesDir, name)
		if err := os.MkdirAll(treePath, 0o755); err != nil {
			return nil, 0, err
		}
		if err := os.WriteFile(filepath.Join(treePath, rc.root+".json"), rc.raw, 0o644); err != nil {
			return nil, 0, err
		}

		ev := jsonFuzzEvaluatedCase{
			name:     name,
			category: rc.category,
			unit:     rc.unit,
			root:     rc.root,
			raw:      rc.raw,
			treeRel:  repoRelative(treePath),
		}
		if ok {
			wireBytes, werr := tablewire.Encode(model, val)
			jsonBytes, jerr := model.Write(val)
			if werr == nil && jerr == nil && len(wireBytes) > 0 {
				ev.accepted = true
				ev.canonWire = wireBytes
				ev.canonJSON = oneText(jsonBytes)
				wirePath := filepath.Join(wiresDir, name+".bin")
				if err := os.WriteFile(wirePath, wireBytes, 0o644); err != nil {
					return nil, 0, err
				}
				ev.wireRel = repoRelative(wirePath)
				acceptedCount++
			} else {
				// If a decoded value violates a file/write invariant (e.g. non-textable),
				// replace input with an explicit syntax refusal so both surfaces stay exact.
				badRaw := append([]byte("{"), rc.raw...)
				if err := os.WriteFile(filepath.Join(treePath, rc.root+".json"), badRaw, 0o644); err != nil {
					return nil, 0, err
				}
				ev.raw = badRaw
			}
		}
		cases = append(cases, ev)
	}

	var b strings.Builder
	b.WriteString("# DERIVED by `harness json-fuzz` — do not commit.\n\n")
	for _, mu := range m.Units {
		if neededUnits[mu.Key] {
			fmt.Fprintf(&b, "unit %s %s\n", mu.Key, strings.Join(mu.Paths, " "))
		}
	}
	for _, c := range cases {
		fmt.Fprintf(&b, "json-hostile %s %s %s %s\n", c.name, c.unit, c.root, c.treeRel)
	}
	for _, c := range cases {
		if c.accepted {
			fmt.Fprintf(&b, "instance %s %s %s %s\n", c.name, c.unit, c.root, c.wireRel)
		}
	}
	if err := writeFileAtomic(filepath.Join(workDir, "manifest.txt"), []byte(b.String())); err != nil {
		return nil, 0, err
	}
	return cases, acceptedCount, nil
}

func runJSONFuzzDriver(d driver, manifestRel, workDir string, cases []jsonFuzzEvaluatedCase, failedPath string) ([]string, error) {
	hostileOut := filepath.Join(workDir, "out", d.lang, "json-hostile")
	writeOut := filepath.Join(workDir, "out", d.lang, "json-write")
	if err := os.MkdirAll(hostileOut, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(writeOut, 0o755); err != nil {
		return nil, err
	}

	code, stderr, err := runDriver(d, manifestRel, "json-hostile", repoRelative(hostileOut))
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return []string{fmt.Sprintf("json-hostile driver exited %d: %s", code, strings.TrimSpace(stderr))}, nil
	}

	hasAccepted := false
	for _, c := range cases {
		if c.accepted {
			hasAccepted = true
			break
		}
	}
	if hasAccepted {
		code, stderr, err = runDriver(d, manifestRel, "json-write", repoRelative(writeOut))
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return []string{fmt.Sprintf("json-write driver exited %d: %s", code, strings.TrimSpace(stderr))}, nil
		}
	}

	var failures []string
	savedFailure := false
	recordFail := func(c jsonFuzzEvaluatedCase, msg string, got []byte) {
		failures = append(failures, fmt.Sprintf("%s [%s %s/%s]: %s (input=%s)",
			c.name, c.category, c.unit, c.root, msg, summarizeJSONInput(c.raw)))
		if !savedFailure && failedPath != "" {
			savedFailure = true
			_ = saveJSONFuzzFailure(failedPath, d.lang, c, msg, got)
		}
	}

	for _, c := range cases {
		ansPath := filepath.Join(hostileOut, c.name)
		gotRaw, rerr := os.ReadFile(ansPath)
		if rerr != nil {
			if _, aerr := os.Stat(ansPath + ".absent"); aerr == nil {
				recordFail(c, "driver answered ABSENT on carried root", nil)
				continue
			}
			recordFail(c, "json-hostile wrote no answer file", nil)
			continue
		}
		gotNorm, nerr := normalizeReadAnswer(gotRaw)
		if nerr != nil {
			recordFail(c, fmt.Sprintf("invalid json-hostile header: %v", nerr), gotRaw)
			continue
		}
		wirePath := ansPath + ".wire"
		if !c.accepted {
			if string(gotNorm) != "refused\n" {
				recordFail(c, fmt.Sprintf("oracle REFUSED input, leg ACCEPTED (%s)", firstLine(gotNorm)), gotNorm)
				continue
			}
			if st, serr := os.Stat(wirePath); serr == nil && st.Size() > 0 {
				recordFail(c, "oracle REFUSED input, leg wrote non-empty .wire", nil)
				continue
			}
			continue
		}

		// Oracle accepted: check FromJson -> ToJson canonical text, FromJson -> Save wire,
		// AND binary Load(oracleWire) -> ToJson (`json-write`).
		wantHostile := append([]byte("read\n"), c.canonJSON...)
		if string(gotNorm) == "refused\n" {
			recordFail(c, "oracle ACCEPTED input, leg REFUSED", gotNorm)
			continue
		}
		if !bytes.Equal(gotNorm, wantHostile) {
			recordFail(c, "FromJson -> ToJson differs from oracle: "+describeTextDiff(c.canonJSON, bytes.TrimPrefix(gotNorm, []byte("read\n"))), gotNorm)
			continue
		}
		gotWire, werr := os.ReadFile(wirePath)
		if werr != nil {
			recordFail(c, "json-hostile wrote no .wire for accepted input", nil)
			continue
		}
		if !bytes.Equal(gotWire, c.canonWire) {
			recordFail(c, "FromJson -> Save wire differs from oracle: "+describeDiff(c.name+".wire", c.canonWire, gotWire), gotWire)
			continue
		}

		gotWriteJSON, jerr := os.ReadFile(filepath.Join(writeOut, c.name+".json"))
		if jerr != nil {
			recordFail(c, "json-write wrote no .json for accepted wire", nil)
			continue
		}
		if !bytes.Equal(gotWriteJSON, c.canonJSON) {
			recordFail(c, "Load(wire) -> ToJson differs from oracle: "+describeTextDiff(c.canonJSON, gotWriteJSON), gotWriteJSON)
			continue
		}
	}
	return failures, nil
}

func saveJSONFuzzFailure(failedPath, lang string, c jsonFuzzEvaluatedCase, msg string, got []byte) error {
	if strings.HasSuffix(failedPath, ".json") || strings.HasSuffix(failedPath, ".bin") || strings.HasSuffix(failedPath, ".txt") {
		if err := os.MkdirAll(filepath.Dir(failedPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(failedPath, c.raw, 0o644)
	}
	dir := filepath.Join(failedPath, lang, c.name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(dir, c.root+".input.json"), c.raw, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "diagnosis.txt"), []byte(msg+"\n"), 0o644)
	if c.accepted {
		_ = os.WriteFile(filepath.Join(dir, "oracle.json"), c.canonJSON, 0o644)
		_ = os.WriteFile(filepath.Join(dir, "oracle.wire"), c.canonWire, 0o644)
	}
	if len(got) > 0 {
		_ = os.WriteFile(filepath.Join(dir, "got.out"), got, 0o644)
	}
	return nil
}

func summarizeJSONInput(raw []byte) string {
	const limit = 96
	s := strconv.QuoteToASCII(string(raw))
	if len(s) > limit {
		return s[:limit] + "...(" + strconv.Itoa(len(raw)) + "B)"
	}
	return s
}

func describeTextDiff(want, got []byte) string {
	limit := min(len(want), len(got))
	pos := limit
	for i := range limit {
		if want[i] != got[i] {
			pos = i
			break
		}
	}
	wStart := max(0, pos-20)
	wEnd := min(len(want), pos+32)
	gStart := max(0, pos-20)
	gEnd := min(len(got), pos+32)
	return fmt.Sprintf("byte %d (len got=%d want=%d): got %s vs want %s",
		pos, len(got), len(want),
		strconv.QuoteToASCII(string(got[gStart:gEnd])),
		strconv.QuoteToASCII(string(want[wStart:wEnd])))
}

func generateJSONFuzzRawCases(m *Manifest, jsonDir string, seed uint64, n int) ([]jsonFuzzRawCase, error) {
	seeds, err := loadJSONFuzzSeedDocs(m, jsonDir)
	if err != nil {
		return nil, err
	}
	targeted := buildTargetedJSONCases()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))

	// Deterministically permute targeted cases under `seed` so even small `--n`
	// exercises every category across seeds, while `--n >= len(targeted)` always
	// includes 100% of the targeted adversarial & boundary bank!
	perm := rng.Perm(len(targeted))
	shuffledTargeted := make([]jsonFuzzRawCase, len(targeted))
	for i, idx := range perm {
		shuffledTargeted[i] = targeted[idx]
	}

	if n < len(shuffledTargeted) {
		// Keep 75% targeted + 25% corpus mutations when n is small.
		tCount := max(1, (n*3)/4)
		out := make([]jsonFuzzRawCase, 0, n)
		out = append(out, shuffledTargeted[:tCount]...)
		for len(out) < n {
			out = append(out, mutateJSONSeedDoc(rng, seeds))
		}
		return out, nil
	}

	out := make([]jsonFuzzRawCase, 0, n)
	out = append(out, shuffledTargeted...)
	for len(out) < n {
		out = append(out, mutateJSONSeedDoc(rng, seeds))
	}
	return out, nil
}

func loadJSONFuzzSeedDocs(m *Manifest, jsonDir string) ([]jsonFuzzSeedDoc, error) {
	allowed := map[jsonFuzzTarget]bool{}
	for _, t := range jsonFuzzTargets {
		allowed[t] = true
	}
	var out []jsonFuzzSeedDoc
	for _, inst := range m.Instances {
		if inst.NoText || inst.WireRefused {
			continue
		}
		if !allowed[jsonFuzzTarget{unit: inst.Unit, root: inst.Root}] {
			continue
		}
		path := filepath.Join(jsonDir, inst.Name+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(raw) > 8192 {
			continue
		}
		out = append(out, jsonFuzzSeedDoc{
			name: inst.Name,
			unit: inst.Unit,
			root: inst.Root,
			raw:  raw,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	if len(out) == 0 {
		return nil, errors.New("no corpus JSON seed instances found")
	}
	return out, nil
}

func buildTargetedJSONCases() []jsonFuzzRawCase {
	var out []jsonFuzzRawCase
	add := func(cat, unit, root, body string) {
		out = append(out, jsonFuzzRawCase{
			category: cat,
			unit:     unit,
			root:     root,
			raw:      []byte(body),
		})
	}
	addBytes := func(cat, unit, root string, body []byte) {
		out = append(out, jsonFuzzRawCase{
			category: cat,
			unit:     unit,
			root:     root,
			raw:      body,
		})
	}

	prof := func(fields string) string {
		return `{"profiles":[{` + fields + `}]}`
	}
	weap := func(fields string) string {
		return `{"weapons":[{` + fields + `}]}`
	}
	fw := func(extra string) string {
		if extra == "" {
			return `{"revision":7}`
		}
		return `{"revision":7,` + extra + `}`
	}

	// ---- 1. Numbers: -0, exponents, subnormals, float32/64 ceilings, ints, fixed/ufixed ----
	add("num-neg-zero-f64", "tabledemo", "RootConfig", prof(`"precision":-0.0,"ratings":[-0.0,-0,0e50,-0.0e-50]`))
	add("num-zero-exponents", "tabledemo", "RootConfig", prof(`"precision":0e100,"experience":0`))
	add("num-neg-zero-int", "featurewitnesses", "Root", `{"revision":-0,"lower":5}`)
	add("num-f64-subnormal-min", "tabledemo", "RootConfig", prof(`"precision":5e-324`))
	add("num-f64-subnormal-neg", "tabledemo", "RootConfig", prof(`"precision":-5e-324`))
	add("num-f64-min-normal", "tabledemo", "RootConfig", prof(`"precision":2.2250738585072014e-308`))
	add("num-f64-max-finite", "tabledemo", "RootConfig", prof(`"precision":1.7976931348623157e+308`))
	add("num-f64-min-finite", "tabledemo", "RootConfig", prof(`"precision":-1.7976931348623157e+308`))
	add("num-f64-overflow-edge", "tabledemo", "RootConfig", prof(`"precision":1.7976931348623159e+308`))
	add("num-f64-overflow-1e309", "tabledemo", "RootConfig", prof(`"precision":1e309`))
	add("num-f64-underflow-zero", "tabledemo", "RootConfig", prof(`"precision":1e-400`))
	add("num-f64-ryu-ties", "tabledemo", "RootConfig", prof(`"precision":9007199254740992.0`))
	add("num-f32-subnormal-min", "tabledemo", "RootConfig", prof(`"ratings":[1.401298464324817e-45,-1.401298464324817e-45,1.17549435e-38,0]`))
	add("num-f32-max-finite", "tabledemo", "RootConfig", prof(`"ratings":[3.4028234663852886e+38,-3.4028234663852886e+38,0,0]`))
	add("num-f32-below-inf-tie", "tabledemo", "RootConfig", prof(`"ratings":[3.4028235677973362e+38,0,0,0]`))
	add("num-f32-at-inf-tie", "tabledemo", "RootConfig", prof(`"ratings":[3.4028235677973366e+38,0,0,0]`))
	add("num-f32-overflow", "tabledemo", "RootConfig", prof(`"ratings":[3.4028236e+38,0,0,0]`))
	add("num-f32-neg-overflow", "tabledemo", "RootConfig", prof(`"ratings":[-3.4028236e+38,0,0,0]`))

	// Integer exact min/max and +-1 across int8..int128 / uint8..uint128 / bits / ranged
	add("num-int-exact-bounds-min", "tabledemo", "RootConfig", prof(`"tilt":-128,"badge":0,"heading":-32768,"port":0,"experience":0,"timestamp":"-9223372036854775808","epoch":"0"`))
	add("num-int-exact-bounds-max", "tabledemo", "RootConfig", prof(`"tilt":127,"badge":255,"heading":32767,"port":65535,"experience":4294967295,"timestamp":"9223372036854775807","epoch":"18446744073709551615"`))
	add("num-int8-under", "tabledemo", "RootConfig", prof(`"tilt":-129`))
	add("num-int8-over", "tabledemo", "RootConfig", prof(`"tilt":128`))
	add("num-uint8-under", "tabledemo", "RootConfig", prof(`"badge":-1`))
	add("num-uint8-over", "tabledemo", "RootConfig", prof(`"badge":256`))
	add("num-int16-under", "tabledemo", "RootConfig", prof(`"heading":-32769`))
	add("num-int16-over", "tabledemo", "RootConfig", prof(`"heading":32768`))
	add("num-uint16-under", "tabledemo", "RootConfig", prof(`"port":-1`))
	add("num-uint16-over", "tabledemo", "RootConfig", prof(`"port":65536`))
	add("num-int32-bounds", "featurewitnesses", "ArrayRoot", `[-2147483648,0,2147483647]`)
	add("num-int32-under", "featurewitnesses", "ArrayRoot", `[-2147483649]`)
	add("num-int32-over", "featurewitnesses", "ArrayRoot", `[2147483648]`)
	add("num-uint32-under", "tabledemo", "RootConfig", prof(`"experience":-1`))
	add("num-uint32-over", "tabledemo", "RootConfig", prof(`"experience":4294967296`))
	add("num-int64-under", "tabledemo", "RootConfig", prof(`"timestamp":"-9223372036854775809"`))
	add("num-int64-over", "tabledemo", "RootConfig", prof(`"timestamp":"9223372036854775808"`))
	add("num-uint64-under", "tabledemo", "RootConfig", prof(`"epoch":"-1"`))
	add("num-uint64-over", "tabledemo", "RootConfig", prof(`"epoch":"18446744073709551616"`))
	add("num-int64-unquoted", "tabledemo", "RootConfig", prof(`"timestamp":42`))
	add("num-uint64-unquoted", "tabledemo", "RootConfig", prof(`"epoch":42`))
	add("num-int64-neg-zero-str", "tabledemo", "RootConfig", prof(`"timestamp":"-0"`))
	add("num-int64-leading-zero-str", "tabledemo", "RootConfig", prof(`"timestamp":"00"`))
	add("num-int64-plus-str", "tabledemo", "RootConfig", prof(`"timestamp":"+7"`))
	add("num-int64-space-str", "tabledemo", "RootConfig", prof(`"timestamp":" 7"`))
	add("num-int64-unicode-escape-str", "tabledemo", "RootConfig", prof(`"timestamp":"\u0034\u0032"`))
	add("num-int32-quoted-wrong", "featurewitnesses", "Root", `{"revision":"7"}`)
	add("num-int32-fractional", "featurewitnesses", "Root", `{"revision":7.5}`)
	add("num-int32-dot-zero", "featurewitnesses", "Root", `{"revision":7.0}`)
	add("num-int32-exp", "featurewitnesses", "Root", `{"revision":1e2}`)
	add("num-ranged-edges-ok", "tabledemo", "RootConfig", weap(`"penetration":0,"channel":63`))
	add("num-ranged-under", "tabledemo", "RootConfig", weap(`"penetration":-1`))
	add("num-ranged-over", "tabledemo", "RootConfig", weap(`"penetration":11`))
	add("num-bits6-over", "tabledemo", "RootConfig", weap(`"channel":64`))
	add("num-bits6-under", "tabledemo", "RootConfig", weap(`"channel":-1`))
	add("num-fw-lower-under", "featurewitnesses", "Root", `{"revision":1,"lower":4}`)

	// 128-bit & fixed/ufixed edges (`scalars SimState`)
	add("num-u128-max", "scalars", "SimState", `{"entity_id":"340282366920938463463374607431768211455","seeds":["0","340282366920938463463374607431768211455"]}`)
	add("num-u128-over", "scalars", "SimState", `{"entity_id":"340282366920938463463374607431768211456"}`)
	add("num-u128-neg", "scalars", "SimState", `{"entity_id":"-1"}`)
	add("num-u128-unquoted", "scalars", "SimState", `{"entity_id":1}`)
	add("num-i128-ranged-bounds", "scalars", "SimState", `{"flux":"-1267650600228229401496703205376","energy":"5000000000"}`)
	add("num-i128-ranged-under", "scalars", "SimState", `{"flux":"-1267650600228229401496703205377"}`)
	add("num-i128-ranged-over", "scalars", "SimState", `{"flux":"1267650600228229401496703205377"}`)
	add("num-i128-type-under", "scalars", "SimState", `{"flux":"-170141183460469231731687303715884105729"}`)
	add("num-fixed-exact-lsb", "scalars", "SimState", `{"tilt":-7.9375,"angle":0.0000152587890625,"position":"-0.0000152587890625","reach":"999999.9999847412109375","axes":{"X":"0.00000000023283064365386962890625","Y":"-0.0","Z":"0.5"}}`)
	add("num-fixed-inexact-sub-lsb", "scalars", "SimState", `{"tilt":0.03125}`)
	add("num-fixed-non-dyadic", "scalars", "SimState", `{"position":"0.1"}`)
	add("num-fixed-over-max", "scalars", "SimState", `{"tilt":7.0625}`)
	add("num-fixed-under-min", "scalars", "SimState", `{"tilt":-8.0625}`)
	add("num-fixed-wide-over", "scalars", "SimState", `{"reach":"1000000.0000152587890625"}`)
	add("num-ufixed-neg", "scalars", "SimState", `{"ratio":-0.0625}`)
	add("num-ufixed-neg-zero", "scalars", "SimState", `{"ratio":-0.0,"span":"-0.0"}`)
	add("num-fixed-bad-grammar-dot", "scalars", "SimState", `{"position":"1."}`)
	add("num-fixed-bad-grammar-lead-dot", "scalars", "SimState", `{"position":".5"}`)
	add("num-fixed-bad-grammar-exp", "scalars", "SimState", `{"position":"1e2"}`)

	// Malformed JSON number grammar
	add("num-leading-plus", "featurewitnesses", "Root", `{"revision":+1}`)
	add("num-leading-zero", "featurewitnesses", "Root", `{"revision":01}`)
	add("num-neg-leading-zero", "featurewitnesses", "Root", `{"revision":-01}`)
	add("num-double-zero", "tabledemo", "RootConfig", prof(`"precision":00.5`))
	add("num-trailing-dot", "tabledemo", "RootConfig", prof(`"precision":1.`))
	add("num-leading-dot", "tabledemo", "RootConfig", prof(`"precision":.5`))
	add("num-neg-leading-dot", "tabledemo", "RootConfig", prof(`"precision":-.5`))
	add("num-bare-nan", "tabledemo", "RootConfig", prof(`"precision":NaN`))
	add("num-bare-inf", "tabledemo", "RootConfig", prof(`"precision":Infinity`))
	add("num-bare-neg-inf", "tabledemo", "RootConfig", prof(`"precision":-Infinity`))
	add("num-string-nan", "tabledemo", "RootConfig", prof(`"precision":"NaN"`))
	add("num-incomplete-exp", "tabledemo", "RootConfig", prof(`"precision":1e`))
	add("num-incomplete-exp-sign", "tabledemo", "RootConfig", prof(`"precision":1e+`))
	add("num-hex-literal", "featurewitnesses", "Root", `{"revision":0x10}`)
	add("num-long-mantissa", "tabledemo", "RootConfig", prof(`"precision":0.`+strings.Repeat("0", 600)+`1`))
	add("num-long-integer", "featurewitnesses", "Root", `{"revision":1`+strings.Repeat("0", 600)+`}`)

	// ---- 2. UTF-8, Surrogates, string(N) Byte Boundaries, #1848 NUL, Escapes ----
	add("utf8-lone-high-d800", "tabledemo", "RootConfig", `{"version_note":"\uD800"}`)
	add("utf8-lone-high-dbff", "tabledemo", "RootConfig", `{"version_note":"\uDBFF"}`)
	add("utf8-lone-low-dc00", "tabledemo", "RootConfig", `{"version_note":"\uDC00"}`)
	add("utf8-lone-low-dfff", "tabledemo", "RootConfig", `{"version_note":"\uDFFF"}`)
	add("utf8-reversed-surrogates", "tabledemo", "RootConfig", `{"version_note":"\uDC00\uD800"}`)
	add("utf8-high-then-ascii", "tabledemo", "RootConfig", `{"version_note":"\uD800a"}`)
	add("utf8-high-then-bmp-escape", "tabledemo", "RootConfig", `{"version_note":"\uD800\u0041"}`)
	add("utf8-high-then-high", "tabledemo", "RootConfig", `{"version_note":"\uD800\uD800"}`)
	add("utf8-lone-surrogate-in-skipped", "featurewitnesses", "Root", fw(`"unknown_str":"ok\uD800bad"`))
	add("utf8-lone-surrogate-in-key", "featurewitnesses", "Root", fw(`"by_name":{"\uD800":1}`))
	add("utf8-valid-astral-pairs", "featurewitnesses", "Root", fw(`"words":["\uD83D\uDE80","\uD800\uDC00","\uDBFF\uDFFF","\ud83d\ude80","🚀"]`))

	// string(16) UTF-8 byte boundary with 4-byte astral (\uD83D\uDE80), 3-byte (€), 2-byte (é)
	add("utf8-bound16-astral-15b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaa\uD83D\uDE80"}`)
	add("utf8-bound16-astral-16b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaaa\uD83D\uDE80"}`)
	add("utf8-bound16-astral-17b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaaaa\uD83D\uDE80"}`)
	add("utf8-bound16-four-rockets-16b", "tabledemo", "RootConfig", `{"version_note":"\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80"}`)
	add("utf8-bound16-four-rockets-plus1-17b", "tabledemo", "RootConfig", `{"version_note":"\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80a"}`)
	add("utf8-bound16-euro-16b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaaaa\u20AC"}`)
	add("utf8-bound16-euro-17b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaaaaa\u20AC"}`)
	add("utf8-bound16-acute-16b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaaaaa\u00E9"}`)
	add("utf8-bound16-acute-17b", "tabledemo", "RootConfig", `{"version_note":"aaaaaaaaaaaaaaa\u00E9"}`)

	// string(8) & total=8 on Root.labels and Root.tags
	add("utf8-labels-total-8b-astral", "featurewitnesses", "Root", fw(`"labels":["\uD83D\uDE80","\uD83D\uDE80"]`))
	add("utf8-labels-total-9b-astral", "featurewitnesses", "Root", fw(`"labels":["\uD83D\uDE80","\uD83D\uDE80","a"]`))
	add("utf8-labels-elem-8b-astral", "featurewitnesses", "Root", fw(`"labels":["aaaa\uD83D\uDE80"]`))
	add("utf8-labels-elem-9b-astral", "featurewitnesses", "Root", fw(`"labels":["aaaaa\uD83D\uDE80"]`))
	add("utf8-tags-key-8b-astral", "featurewitnesses", "Root", fw(`"tags":{"aaaa\uD83D\uDE80":9}`))
	add("utf8-tags-key-9b-astral", "featurewitnesses", "Root", fw(`"tags":{"aaaaa\uD83D\uDE80":9}`))

	// \u0000 NUL in declared strings vs skipped unknown strings (#1848)
	add("utf8-nul-declared-fixed-str", "tabledemo", "RootConfig", `{"version_note":"ab\u0000cd"}`)
	add("utf8-nul-declared-unbounded-str", "featurewitnesses", "Root", fw(`"words":["ab\u0000cd"]`))
	add("utf8-nul-declared-blob-str", "blobdemo", "Catalog", `{"name":"ok","note":"ab\u0000cd"}`)
	add("utf8-nul-declared-map-key", "featurewitnesses", "Root", fw(`"by_name":{"a\u0000b":1}`))
	add("utf8-nul-skipped-unknown-root-var", "featurewitnesses", "Root", fw(`"unknown_nul":"ab\u0000cd"`))
	add("utf8-nul-skipped-unknown-nested-var", "featurewitnesses", "Root", fw(`"unknown_nested":{"arr":["\u0000",{"s":"x\u0000y"}]}`))
	add("utf8-nul-skipped-unknown-root-fixed", "tabledemo", "RootConfig", `{"version_note":"ok","unknown_nul":"ab\u0000cd"}`)
	add("utf8-nul-unknown-object-key", "featurewitnesses", "Root", fw(`"unk\u0000key":"val"`))

	// Valid escapes canonical roundtrip + U+FFFD vs raw invalid UTF-8 / control bytes
	add("utf8-all-valid-escapes", "featurewitnesses", "Root", fw(`"words":["\"\\\/\b\f\n\r\t\u0001\u001f\u007f\u0080\u07ff\ufffd"]`))
	addBytes("utf8-literal-fffd", "featurewitnesses", "Root", []byte("{\"revision\":7,\"words\":[\"\xef\xbf\xbd\"]}"))
	addBytes("utf8-raw-control-0x01", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\x01b\"}"))
	addBytes("utf8-raw-control-tab", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\tb\"}"))
	addBytes("utf8-raw-control-lf", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\nb\"}"))
	addBytes("utf8-raw-control-cr", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\rb\"}"))
	addBytes("utf8-raw-control-0x1f", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\x1fb\"}"))
	addBytes("utf8-raw-0xff", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\xffb\"}"))
	addBytes("utf8-raw-overlong-nul", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\xc0\x80b\"}"))
	addBytes("utf8-raw-overlong-slash", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\xc0\xafb\"}"))
	addBytes("utf8-raw-overlong-3b", "tabledemo", "RootConfig", []byte("{\"version_note\":\"a\xe0\x80\x80b\"}"))
	addBytes("utf8-raw-cesu8-pair", "tabledemo", "RootConfig", []byte("{\"version_note\":\"\xed\xa0\xbd\xed\xb8\x80\"}"))
	addBytes("utf8-raw-cesu8-single", "tabledemo", "RootConfig", []byte("{\"version_note\":\"\xed\xa0\x80\"}"))
	addBytes("utf8-raw-trunc-2b", "tabledemo", "RootConfig", []byte("{\"version_note\":\"\xc3\"}"))
	addBytes("utf8-raw-trunc-3b", "tabledemo", "RootConfig", []byte("{\"version_note\":\"\xe2\x82\"}"))
	addBytes("utf8-raw-trunc-4b", "tabledemo", "RootConfig", []byte("{\"version_note\":\"\xf0\x9f\x9a\"}"))
	addBytes("utf8-raw-above-10ffff", "tabledemo", "RootConfig", []byte("{\"version_note\":\"\xf4\x90\x80\x80\"}"))
	addBytes("utf8-raw-invalid-in-skipped", "featurewitnesses", "Root", []byte("{\"revision\":7,\"unk\":\"a\xffb\"}"))
	add("utf8-bad-escape-hex", "tabledemo", "RootConfig", `{"version_note":"\x41"}`)
	add("utf8-bad-escape-a", "tabledemo", "RootConfig", `{"version_note":"\a"}`)
	add("utf8-bad-escape-zero", "tabledemo", "RootConfig", `{"version_note":"\0"}`)
	add("utf8-bad-escape-quote", "tabledemo", "RootConfig", `{"version_note":"\'"}`)
	add("utf8-bad-escape-short-u", "tabledemo", "RootConfig", `{"version_note":"\u123"}`)
	add("utf8-bad-escape-nonhex-u", "tabledemo", "RootConfig", `{"version_note":"\u123G"}`)
	add("utf8-unterminated-string", "tabledemo", "RootConfig", `{"version_note":"abc`)
	add("utf8-trailing-backslash-eof", "tabledemo", "RootConfig", `{"version_note":"abc\`)

	// ---- 3. Duplicate Keys (Root, Nested, Map, Escape-Equivalent, Skipped Objects/Arrays) ----
	add("dup-root-known", "featurewitnesses", "Root", `{"revision":1,"revision":2}`)
	add("dup-root-known-same-val", "featurewitnesses", "Root", `{"revision":7,"revision":7}`)
	add("dup-root-escape-equiv", "featurewitnesses", "Root", `{"revision":1,"\u0072evision":2}`)
	add("dup-root-unknown", "featurewitnesses", "Root", `{"revision":7,"unk":1,"unk":2}`)
	add("dup-root-unknown-escape", "featurewitnesses", "Root", `{"revision":7,"unk":1,"\u0075nk":2}`)
	add("dup-nested-table", "featurewitnesses", "Root", fw(`"optional":{"child":{"value":1,"value":2}}`))
	add("dup-nested-fixed", "tabledemo", "RootConfig", weap(`"penetration":1,"penetration":2`))
	add("dup-map-tags", "featurewitnesses", "Root", fw(`"tags":{"k":1,"k":2}`))
	add("dup-map-tags-escape", "featurewitnesses", "Root", fw(`"tags":{"k":1,"\u006b":2}`))
	add("dup-map-unbounded", "featurewitnesses", "Root", fw(`"by_name":{"alpha":1,"\u0061lpha":2}`))
	add("dup-skipped-unknown-obj", "featurewitnesses", "Root", fw(`"unk":{"a":1,"a":2}`))
	add("dup-skipped-unknown-obj-escape", "featurewitnesses", "Root", fw(`"unk":{"a":1,"\u0061":2}`))
	add("dup-skipped-unknown-arr-obj", "featurewitnesses", "Root", fw(`"unk":[0,{"x":true,"x":false}]`))
	add("dup-skipped-unknown-deep", "featurewitnesses", "Root", fw(`"unk":{"lvl1":[{"lvl2":{"dup":null,"dup":1}}]}`))
	add("dup-skipped-fixed-root", "tabledemo", "RootConfig", `{"version_note":"v1","unk":{"k":1,"k":2}}`)

	// ---- 4. Canonical Ordering: Shuffled Fields & UTF-8 vs UTF-16 Map Keys ----
	add("order-shuffled-fields", "featurewitnesses", "Root",
		`{"words":["z","a"],"by_name":{"b":2,"a":1},"labels":["two","one"],"optional":{"child":{"value":9}},"tags":{"z":9,"a":1},"lower":8,"at":"2026-11-02T12:34:56Z","revision":42}`)
	// U+FF01 (EF BC 81 in UTF-8, FF01 in UTF-16) vs U+1F680 (F0 9F 9A 80 in UTF-8, D83D DE80 in UTF-16)!
	add("order-map-utf8-vs-utf16", "featurewitnesses", "Root",
		fw(`"by_name":{"\uD83D\uDE80":1,"\uFF01":2,"~":3,"\u0080":4,"a":5,"":6}`))
	add("order-mapdemo-utf8-vs-utf16", "mapdemo", "Text",
		`{"entries":{"\uD83D\uDE80":"rocket","\uFF01":"bang","b":"two","a":"one"}}`)

	// ---- 5. Base64 (`blobdemo Catalog`, `tabledemo RootConfig.profiles[0].icon`) ----
	add("b64-valid-empty-and-pads", "blobdemo", "Catalog", `{"name":"b","thumb":"","head":{"name":"h","kind":1,"data":"AA==","caption":"","next":{"name":"t","kind":2,"data":"AAA=","caption":null,"next":null}},"alias":"AAAA"}`)
	add("b64-valid-16b-exact", "tabledemo", "RootConfig", prof(`"icon":"AAAAAAAAAAAAAAAAAAAAAA=="`))
	add("b64-over-17b-fixed16", "tabledemo", "RootConfig", prof(`"icon":"AAAAAAAAAAAAAAAAAAAAAAA="`))
	add("b64-json-unicode-escaped", "blobdemo", "Catalog", `{"name":"esc","thumb":"\u0041\u0051\u0049\u0044"}`)
	add("b64-dirty-pad-1byte-AB", "blobdemo", "Catalog", `{"name":"bad","thumb":"AB=="}`)
	add("b64-dirty-pad-1byte-AP", "tabledemo", "RootConfig", prof(`"icon":"AP=="`))
	add("b64-dirty-pad-2byte-AAB", "blobdemo", "Catalog", `{"name":"bad","thumb":"AAB="}`)
	add("b64-dirty-pad-2byte-AAD", "tabledemo", "RootConfig", prof(`"icon":"AAD="`))
	add("b64-missing-pad-2chars", "blobdemo", "Catalog", `{"name":"bad","thumb":"AA"}`)
	add("b64-missing-pad-3chars", "blobdemo", "Catalog", `{"name":"bad","thumb":"AAA"}`)
	add("b64-single-char", "blobdemo", "Catalog", `{"name":"bad","thumb":"A"}`)
	add("b64-single-char-eq", "blobdemo", "Catalog", `{"name":"bad","thumb":"A="}`)
	add("b64-triple-eq", "blobdemo", "Catalog", `{"name":"bad","thumb":"A==="}`)
	add("b64-quad-eq", "blobdemo", "Catalog", `{"name":"bad","thumb":"===="}`)
	add("b64-mid-pad", "blobdemo", "Catalog", `{"name":"bad","thumb":"AA==AAAA"}`)
	add("b64-interior-space", "blobdemo", "Catalog", `{"name":"bad","thumb":"AQ ID"}`)
	add("b64-interior-newline", "blobdemo", "Catalog", `{"name":"bad","thumb":"AQ\nID"}`)
	add("b64-urlsafe-alphabet", "blobdemo", "Catalog", `{"name":"bad","thumb":"-_8="}`)

	// ---- 6. RFC3339 Timestamps (`featurewitnesses Root.at`) ----
	add("ts-epoch-zero", "featurewitnesses", "Root", fw(`"at":"1970-01-01T00:00:00Z"`))
	add("ts-leap-year-2024", "featurewitnesses", "Root", fw(`"at":"2024-02-29T23:59:59.999999999Z"`))
	add("ts-leap-century-2000", "featurewitnesses", "Root", fw(`"at":"2000-02-29T00:00:00Z"`))
	add("ts-pos-offset", "featurewitnesses", "Root", fw(`"at":"2026-11-02T12:34:56+05:30"`))
	add("ts-neg-offset", "featurewitnesses", "Root", fw(`"at":"2026-11-02T12:34:56-08:00"`))
	add("ts-frac-trailing-zeros", "featurewitnesses", "Root", fw(`"at":"2026-11-02T12:34:56.500000000Z"`))
	add("ts-unicode-escaped-char", "featurewitnesses", "Root", fw(`"at":"\u0032026-11-02T12:34:56Z"`))
	add("ts-lowercase-tz", "featurewitnesses", "Root", fw(`"at":"2026-11-02t12:34:56z"`))
	add("ts-space-sep", "featurewitnesses", "Root", fw(`"at":"2026-11-02 12:34:56Z"`))
	add("ts-neg-zero-offset", "featurewitnesses", "Root", fw(`"at":"2026-11-02T12:34:56-00:00"`))
	add("ts-non-leap-2026-feb29", "featurewitnesses", "Root", fw(`"at":"2026-02-29T00:00:00Z"`))
	add("ts-non-leap-1900-feb29", "featurewitnesses", "Root", fw(`"at":"1900-02-29T00:00:00Z"`))
	add("ts-april-31", "featurewitnesses", "Root", fw(`"at":"2026-04-31T00:00:00Z"`))
	add("ts-month-00", "featurewitnesses", "Root", fw(`"at":"2026-00-01T00:00:00Z"`))
	add("ts-month-13", "featurewitnesses", "Root", fw(`"at":"2026-13-01T00:00:00Z"`))
	add("ts-day-00", "featurewitnesses", "Root", fw(`"at":"2026-01-00T00:00:00Z"`))
	add("ts-day-32", "featurewitnesses", "Root", fw(`"at":"2026-01-32T00:00:00Z"`))
	add("ts-hour-24", "featurewitnesses", "Root", fw(`"at":"2026-01-01T24:00:00Z"`))
	add("ts-minute-60", "featurewitnesses", "Root", fw(`"at":"2026-01-01T00:60:00Z"`))
	add("ts-leap-second-60", "featurewitnesses", "Root", fw(`"at":"2026-01-01T23:59:60Z"`))
	add("ts-trailing-dot", "featurewitnesses", "Root", fw(`"at":"2026-01-01T00:00:00.Z"`))
	add("ts-10-frac-digits", "featurewitnesses", "Root", fw(`"at":"2026-01-01T00:00:00.1234567890Z"`))
	add("ts-offset-hour-24", "featurewitnesses", "Root", fw(`"at":"2026-01-01T00:00:00+24:00"`))
	add("ts-offset-min-60", "featurewitnesses", "Root", fw(`"at":"2026-01-01T00:00:00+00:60"`))
	add("ts-int64-ns-overflow", "featurewitnesses", "Root", fw(`"at":"2263-01-01T00:00:00Z"`))
	add("ts-int64-ns-underflow", "featurewitnesses", "Root", fw(`"at":"1677-01-01T00:00:00Z"`))
	add("ts-bare-integer", "featurewitnesses", "Root", fw(`"at":0`))

	// ---- 7. Evolution, Required, Unions, Enums, Flags, json="array" ----
	add("evo-missing-required", "featurewitnesses", "Root", `{}`)
	add("evo-null-required", "featurewitnesses", "Root", `{"revision":null}`)
	add("evo-unknown-all-shapes", "featurewitnesses", "Root",
		fw(`"u_null":null,"u_bool":true,"u_num":-12.5e3,"u_str":"skip","u_arr":[1,true,null,{"x":"y"}],"u_obj":{"nested":{"k":[]}}`))
	add("evo-unknown-on-fixed", "tabledemo", "RootConfig", `{"version_note":"ok","u_obj":{"a":[1,2,3]}}`)
	add("evo-enum-unknown", "tabledemo", "RootConfig", prof(`"loadout":{"grade":"Platinum"}`))
	add("evo-enum-nul-suffix", "tabledemo", "RootConfig", prof(`"loadout":{"grade":"Gold\u0000"}`))
	add("evo-flags-nul-suffix", "tabledemo", "RootConfig", prof(`"loadout":{"perks":["Turbo\u0000"]}`))
	add("evo-union-arm-key-nul", "messagedemo", "ToolMessage", `{"sequence":9,"body":{"ping\u0000":{"nonce":0}},"history":[]}`)
	add("evo-skipped-node-in-ptr-array", "streamdemo", "Feed",
		`{"id":3,"frame":{"chunk":{"data":"AQ==","unk_skip":{"&node":1,"data":"Ag=="},"links":[{"&node":3,"data":"CQ==","next":null,"links":[]}]}},"parts":[{"&node":1},null,{"&node":3}],"pair":[null,{"&node":3}]}`)
	add("evo-enum-escaped-known", "tabledemo", "RootConfig", prof(`"loadout":{"grade":"\u0047old"}`))
	add("evo-enum-number", "tabledemo", "RootConfig", prof(`"loadout":{"grade":1}`))
	add("evo-flags-out-of-order", "tabledemo", "RootConfig", prof(`"loadout":{"perks":["Turbo","Shielded"]}`))
	add("evo-flags-duplicate", "tabledemo", "RootConfig", prof(`"loadout":{"perks":["Cloaked","Cloaked"]}`))
	add("evo-flags-escaped-dup", "tabledemo", "RootConfig", prof(`"loadout":{"perks":["Cloaked","\u0043loaked"]}`))
	add("evo-flags-unknown", "tabledemo", "RootConfig", prof(`"loadout":{"perks":["Laser"]}`))
	add("evo-union-empty-none", "tabledemo", "RootConfig", weap(`"effect":{}`))
	add("evo-union-two-arms", "tabledemo", "RootConfig", weap(`"effect":{"buff":{"multiplier":2.0},"debuff":{"amount":5}}`))
	add("evo-union-unknown-arm", "tabledemo", "RootConfig", weap(`"effect":{"laser":{"power":9}}`))
	add("evo-union-null", "tabledemo", "RootConfig", weap(`"effect":null`))
	add("evo-union-wrong-payload", "tabledemo", "RootConfig", weap(`"effect":{"buff":123}`))
	add("evo-keyed-enum-unknown-key", "scalars", "SimState", `{"axes":{"W":"1.0"}}`)
	add("evo-keyed-enum-dup-escape", "scalars", "SimState", `{"axes":{"X":"1.0","\u0058":"2.0"}}`)
	add("arrayroot-empty", "featurewitnesses", "ArrayRoot", `[]`)
	add("arrayroot-object-wrong", "featurewitnesses", "ArrayRoot", `{}`)
	add("arrayroot-null-wrong", "featurewitnesses", "ArrayRoot", `null`)
	add("arrayroot-trailing-comma", "featurewitnesses", "ArrayRoot", `[1,2,]`)
	add("arrayroot-elem-null", "featurewitnesses", "ArrayRoot", `[1,null]`)

	// ---- 8. BOM, Comments, Whitespace, Deep Nesting & Trailing Garbage ----
	addBytes("bom-valid-leading", "featurewitnesses", "Root", []byte("\xef\xbb\xbf{\"revision\":9}"))
	addBytes("bom-valid-leading-arrayroot", "featurewitnesses", "ArrayRoot", []byte("\xef\xbb\xbf[1,2,3]"))
	addBytes("bom-after-space", "featurewitnesses", "Root", []byte(" \xef\xbb\xbf{\"revision\":9}"))
	addBytes("bom-double", "featurewitnesses", "Root", []byte("\xef\xbb\xbf\xef\xbb\xbf{\"revision\":9}"))
	addBytes("bom-truncated-2b", "featurewitnesses", "Root", []byte("\xef\xbb{\"revision\":9}"))
	addBytes("bom-trailing", "featurewitnesses", "Root", []byte("{\"revision\":9}\xef\xbb\xbf"))
	addBytes("bom-utf16le", "featurewitnesses", "Root", []byte("\xff\xfe{\"revision\":9}"))
	add("comment-line-and-block-valid", "featurewitnesses", "Root",
		"// header 🚀\n{\n  /* before key */ \"revision\" /* mid */ : // val\n  11 /* after */\n}\n// trailing")
	add("comment-unterminated-block", "featurewitnesses", "Root", "{\"revision\":11} /* unterminated")
	addBytes("comment-invalid-utf8-line", "featurewitnesses", "Root", []byte("// bad \xff utf8\n{\"revision\":11}\n"))
	addBytes("comment-invalid-utf8-block", "featurewitnesses", "Root", []byte("/* bad \xc0\x80 */{\"revision\":11}"))
	addBytes("comment-nul-byte", "featurewitnesses", "Root", []byte("// nul \x00 here\n{\"revision\":11}\n"))
	addBytes("ws-vertical-tab", "featurewitnesses", "Root", []byte("\x0b{\"revision\":7}"))
	addBytes("ws-form-feed", "featurewitnesses", "Root", []byte("\x0c{\"revision\":7}"))
	addBytes("ws-nbsp", "featurewitnesses", "Root", []byte("\xc2\xa0{\"revision\":7}"))
	add("syntax-empty", "featurewitnesses", "Root", ``)
	add("syntax-ws-only", "featurewitnesses", "Root", "   \n\t\r  ")
	add("syntax-trailing-token-num", "featurewitnesses", "Root", `{"revision":7} 0`)
	add("syntax-trailing-brace", "featurewitnesses", "Root", `{"revision":7}{}`)
	add("syntax-trailing-comma-obj", "featurewitnesses", "Root", `{"revision":7,}`)
	add("syntax-leading-comma-obj", "featurewitnesses", "Root", `{,"revision":7}`)
	add("syntax-missing-colon", "featurewitnesses", "Root", `{"revision" 7}`)
	add("syntax-single-quotes", "featurewitnesses", "Root", `{'revision':7}`)
	add("syntax-unquoted-key", "featurewitnesses", "Root", `{revision:7}`)

	for _, depth := range []int{16, 32, 63, 64, 65, 128, 200} {
		arrNest := strings.Repeat("[", depth) + strings.Repeat("]", depth)
		add(fmt.Sprintf("nest-unknown-array-d%d", depth), "featurewitnesses", "Root", fw(`"u":`+arrNest))
		objNest := strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth)
		add(fmt.Sprintf("nest-unknown-obj-d%d", depth), "featurewitnesses", "Root", fw(`"u":`+objNest))
	}

	// ---- 9. Reserved `&`-Prefix Keys & `&node` Graph Label Traps (SPEC-TABLES §16.7, #4408 jf00584) ----
	add("amp-feed-unknown-amp-key", "streamdemo", "Feed", `{"id":1,"&node_extra":1}`)
	add("amp-feed-unknown-amp-first", "streamdemo", "Feed", `{"&node2":1,"id":1}`)
	add("amp-feed-bare-ampersand", "streamdemo", "Feed", `{"&":1,"id":1}`)
	add("amp-feed-escaped-amp-key", "streamdemo", "Feed", `{"id":1,"\u0026node_extra":1}`)
	add("amp-feed-node-second-key", "streamdemo", "Feed", `{"id":1,"&node":1}`)
	add("amp-feed-child-node-second-key", "streamdemo", "Feed",
		`{"id":1,"frame":{"chunk":{"data":"AQ==","&node":1,"next":null,"links":[]}},"parts":[],"pair":[null,null]}`)
	add("amp-feed-node-zero", "streamdemo", "Feed", `{"&node":0,"id":1}`)
	add("amp-feed-node-neg", "streamdemo", "Feed", `{"&node":-1,"id":1}`)
	add("amp-feed-node-string", "streamdemo", "Feed", `{"&node":"1","id":1}`)
	add("amp-feed-node-float", "streamdemo", "Feed", `{"&node":1.0,"id":1}`)
	add("amp-feed-dangling-ref", "streamdemo", "Feed", `{"id":1,"parts":[{"&node":99}],"pair":[null,null]}`)
	add("amp-feed-dup-label", "streamdemo", "Feed",
		`{"id":1,"parts":[{"&node":1,"data":"AQ==","next":null,"links":[]},{"&node":1,"data":"Ag==","next":null,"links":[]}],"pair":[null,null]}`)
	add("amp-scene-unknown-amp-key", "graphdemo", "Scene", `{"&other":1}`)
	add("amp-fw-unknown-amp-key", "featurewitnesses", "Root", `{"revision":7,"&reserved":1}`)
	add("amp-fixed-root-amp-node", "tabledemo", "RootConfig", `{"&node":1,"version_note":"v1"}`)
	add("amp-fixed-root-unknown-amp", "tabledemo", "RootConfig", `{"version_note":"v1","&bad":1}`)

	// ---- 10. Embedded U+0000 & Unpaired Surrogates in Keys and Values (SPEC-TABLES §16.3, jf00461) ----
	add("nul-scene-pointer-first-key", "graphdemo", "Scene",
		`{"name":"tree","version":2,"head":{"value":1,"name":"a","next":{"value\u0000":2,"name":"","next":null}}}`)
	add("nul-scene-pointer-amp-node-key", "graphdemo", "Scene",
		`{"name":"tree","head":{"&node\u0000":1,"value":1,"name":"a","next":null}}`)
	add("nul-scene-pointer-unknown-first-key", "graphdemo", "Scene",
		`{"name":"tree","head":{"nope\u0000":1,"value":1,"name":"a","next":null}}`)
	add("nul-scene-root-known-prefix-key", "graphdemo", "Scene", `{"version\u0000":2,"name":"tree"}`)
	add("nul-scene-skipped-nested-key", "graphdemo", "Scene", `{"name":"tree","unk":{"k\u0000":1}}`)
	add("nul-scene-skipped-nested-val", "graphdemo", "Scene", `{"name":"tree","unk":{"k":"a\u0000b"}}`)
	add("nul-scene-string-val", "graphdemo", "Scene", `{"name":"tr\u0000ee","version":2}`)
	add("nul-fixed-type-field-key", "tabledemo", "RootConfig", prof(`"loadout":{"grade\u0000":"Gold"}`))
	add("nul-fw-map-key", "featurewitnesses", "Root", fw(`"names":{"k\u0000":1}`))
	add("surrogate-scene-pointer-first-key", "graphdemo", "Scene",
		`{"name":"tree","head":{"value\ud800":1,"name":"a","next":null}}`)
	add("surrogate-trail-scene-pointer-first-key", "graphdemo", "Scene",
		`{"name":"tree","head":{"value\udfff":1,"name":"a","next":null}}`)
	add("surrogate-scene-root-key", "graphdemo", "Scene", `{"name\ud800":"tree","version":2}`)
	add("surrogate-scene-string-val", "graphdemo", "Scene", `{"name":"a\ud800b","version":2}`)
	add("surrogate-fixed-type-field-key", "tabledemo", "RootConfig", prof(`"loadout":{"grade\ud800":"Gold"}`))

	return out
}

func mutateJSONSeedDoc(rng *rand.Rand, seeds []jsonFuzzSeedDoc) jsonFuzzRawCase {
	base := seeds[rng.IntN(len(seeds))]
	op := rng.IntN(15)
	switch op {
	case 0:
		return jsonFuzzRawCase{
			category: "mut-shuffle-keys",
			unit:     base.unit,
			root:     base.root,
			raw:      shuffleJSONValueBytes(rng, base.raw, false, false),
		}
	case 1:
		return jsonFuzzRawCase{
			category: "mut-shuffle-inject-unknown",
			unit:     base.unit,
			root:     base.root,
			raw:      shuffleJSONValueBytes(rng, base.raw, true, false),
		}
	case 2:
		return jsonFuzzRawCase{
			category: "mut-unicode-escape-strings",
			unit:     base.unit,
			root:     base.root,
			raw:      escapeRandomJSONStrings(rng, shuffleJSONValueBytes(rng, base.raw, false, false)),
		}
	case 3:
		return jsonFuzzRawCase{
			category: "mut-bom-and-comments",
			unit:     base.unit,
			root:     base.root,
			raw:      decorateValidJSONCommentsBOM(rng, shuffleJSONValueBytes(rng, base.raw, false, false)),
		}
	case 4:
		return jsonFuzzRawCase{
			category: "mut-dup-key-anywhere",
			unit:     base.unit,
			root:     base.root,
			raw:      injectDuplicateKeyInJSON(rng, base.raw, false),
		}
	case 5:
		return jsonFuzzRawCase{
			category: "mut-dup-key-escaped",
			unit:     base.unit,
			root:     base.root,
			raw:      injectDuplicateKeyInJSON(rng, base.raw, true),
		}
	case 6:
		return jsonFuzzRawCase{
			category: "mut-dup-in-skipped-unknown",
			unit:     base.unit,
			root:     base.root,
			raw:      shuffleJSONValueBytes(rng, base.raw, true, true),
		}
	case 7:
		return jsonFuzzRawCase{
			category: "mut-number-edge",
			unit:     base.unit,
			root:     base.root,
			raw:      mutateJSONNumberToken(rng, base.raw),
		}
	case 8:
		return jsonFuzzRawCase{
			category: "mut-string-surrogate-or-nul",
			unit:     base.unit,
			root:     base.root,
			raw:      mutateJSONStringToken(rng, base.raw),
		}
	case 9:
		return jsonFuzzRawCase{
			category: "mut-raw-invalid-utf8",
			unit:     base.unit,
			root:     base.root,
			raw:      injectRawInvalidUTF8(rng, base.raw),
		}
	case 10:
		return jsonFuzzRawCase{
			category: "mut-truncate",
			unit:     base.unit,
			root:     base.root,
			raw:      truncateJSONBytes(rng, base.raw),
		}
	case 11:
		return jsonFuzzRawCase{
			category: "mut-trailing-garbage",
			unit:     base.unit,
			root:     base.root,
			raw:      appendTrailingGarbage(rng, base.raw),
		}
	case 12:
		return jsonFuzzRawCase{
			category: "mut-delimiter-corrupt",
			unit:     base.unit,
			root:     base.root,
			raw:      corruptJSONDelimiter(rng, base.raw),
		}
	case 13:
		return jsonFuzzRawCase{
			category: "mut-reserved-ampersand-key",
			unit:     base.unit,
			root:     base.root,
			raw:      injectReservedAmpersandKey(rng, base.raw),
		}
	default:
		// Combine valid key-shuffling + unicode escapes + BOM/comments in one pass!
		step := shuffleJSONValueBytes(rng, base.raw, rng.IntN(2) == 0, false)
		step = escapeRandomJSONStrings(rng, step)
		step = decorateValidJSONCommentsBOM(rng, step)
		return jsonFuzzRawCase{
			category: "mut-combo-canonical",
			unit:     base.unit,
			root:     base.root,
			raw:      step,
		}
	}
}

func shuffleJSONValueBytes(rng *rand.Rand, raw []byte, injectUnknown, dupInUnknown bool) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return raw
	}
	var buf bytes.Buffer
	renderShuffledJSON(rng, &buf, val, 0, injectUnknown, dupInUnknown)
	return buf.Bytes()
}

func renderShuffledJSON(rng *rand.Rand, buf *bytes.Buffer, v any, depth int, injectUnknown, dupInUnknown bool) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		buf.WriteByte('{')
		wrote := 0
		if injectUnknown && depth == 0 {
			unkName := fmt.Sprintf("__fuzz_unk_%d", rng.IntN(100))
			if dupInUnknown {
				fmt.Fprintf(buf, "%q:{\"k\":1,\"\\u006b\":2}", unkName)
			} else {
				switch rng.IntN(4) {
				case 0:
					fmt.Fprintf(buf, "%q:{\"skip_nul\":\"a\\u0000b\",\"arr\":[true,null,-0.0]}", unkName)
				case 1:
					fmt.Fprintf(buf, "%q:[[[]]]", unkName)
				case 2:
					fmt.Fprintf(buf, "%q:\"\\uD83D\\uDE80\"", unkName)
				default:
					fmt.Fprintf(buf, "%q:0e25", unkName)
				}
			}
			wrote++
		}
		for _, k := range keys {
			if wrote > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			renderShuffledJSON(rng, buf, x[k], depth+1, false, false)
			wrote++
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, elem := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			renderShuffledJSON(rng, buf, elem, depth+1, false, false)
		}
		buf.WriteByte(']')
	case json.Number:
		s := x.String()
		if s == "0" && rng.IntN(3) == 0 {
			buf.WriteString("-0")
			return
		}
		if s == "0.0" && rng.IntN(3) == 0 {
			buf.WriteString("-0.0")
			return
		}
		buf.WriteString(s)
	default:
		b, _ := json.Marshal(x)
		buf.Write(b)
	}
}

func escapeRandomJSONStrings(rng *rand.Rand, raw []byte) []byte {
	var out bytes.Buffer
	inStr := false
	escaped := false
	for i := range raw {
		b := raw[i]
		if !inStr {
			out.WriteByte(b)
			if b == '"' {
				inStr = true
			}
			continue
		}
		if escaped {
			out.WriteByte(b)
			escaped = false
			continue
		}
		switch b {
		case '\\':
			out.WriteByte(b)
			escaped = true
		case '"':
			out.WriteByte(b)
			inStr = false
		default:
			if b >= 0x20 && b < 0x7f && rng.IntN(4) == 0 {
				fmt.Fprintf(&out, "\\u%04x", b)
			} else {
				out.WriteByte(b)
			}
		}
	}
	return out.Bytes()
}

func decorateValidJSONCommentsBOM(rng *rand.Rand, raw []byte) []byte {
	var out bytes.Buffer
	if rng.IntN(2) == 0 {
		out.WriteString("\xef\xbb\xbf")
	}
	if rng.IntN(2) == 0 {
		out.WriteString("// fuzz comment 🚀\n")
	}
	out.WriteString("/* head */\t")
	out.Write(raw)
	out.WriteString("\r\n/* tail */\n")
	return out.Bytes()
}

func injectDuplicateKeyInJSON(rng *rand.Rand, raw []byte, viaEscape bool) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return append([]byte(`{"dup":1,"dup":2}`), raw...)
	}
	m, ok := val.(map[string]any)
	if !ok || len(m) == 0 {
		return []byte(`{"dup":1,"dup":2}`)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pick := keys[rng.IntN(len(keys))]
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, _ := json.Marshal(m[k])
		buf.Write(vb)
	}
	buf.WriteByte(',')
	if viaEscape && len(pick) > 0 && pick[0] < 0x80 {
		rest, _ := json.Marshal(pick[1:])
		fmt.Fprintf(&buf, "\"\\u%04x%s:", pick[0], string(rest[1:]))
	} else {
		kb, _ := json.Marshal(pick)
		buf.Write(kb)
		buf.WriteByte(':')
	}
	vb, _ := json.Marshal(m[pick])
	buf.Write(vb)
	buf.WriteByte('}')
	return buf.Bytes()
}

func mutateJSONNumberToken(rng *rand.Rand, raw []byte) []byte {
	replacements := []string{
		"-0", "-0.0", "0e50", "+1", "00", "1.", ".5", "NaN", "Infinity",
		"99999999999999999999999999999999999999999", "-99999999999999999999999999999999999999999",
		"1.7976931348623159e+308", "3.4028236e+38", "1.5",
	}
	inStr := false
	escaped := false
	type span struct{ start, end int }
	var spans []span
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			switch b {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		if b == '"' {
			inStr = true
			continue
		}
		if (b >= '0' && b <= '9') || b == '-' {
			j := i + 1
			for j < len(raw) && ((raw[j] >= '0' && raw[j] <= '9') || raw[j] == '.' || raw[j] == 'e' || raw[j] == 'E' || raw[j] == '+' || raw[j] == '-') {
				j++
			}
			spans = append(spans, span{i, j})
			i = j - 1
		}
	}
	if len(spans) == 0 {
		return append([]byte("+"), raw...)
	}
	s := spans[rng.IntN(len(spans))]
	rep := replacements[rng.IntN(len(replacements))]
	var out bytes.Buffer
	out.Write(raw[:s.start])
	out.WriteString(rep)
	out.Write(raw[s.end:])
	return out.Bytes()
}

func mutateJSONStringToken(rng *rand.Rand, raw []byte) []byte {
	injections := []string{
		`\uD800`, `\uDC00`, `\uDC00\uD800`, `\uD800a`, `\u0000`,
		`\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80\uD83D\uDE80`,
		`\x41`, `\u123G`, `\0`,
	}
	var quotes []int
	inStr := false
	escaped := false
	for i := range raw {
		b := raw[i]
		if !inStr {
			if b == '"' {
				inStr = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '"' {
			quotes = append(quotes, i)
			inStr = false
		}
	}
	if len(quotes) == 0 {
		return raw
	}
	pos := quotes[rng.IntN(len(quotes))]
	inj := injections[rng.IntN(len(injections))]
	var out bytes.Buffer
	out.Write(raw[:pos])
	out.WriteString(inj)
	out.Write(raw[pos:])
	return out.Bytes()
}

func injectRawInvalidUTF8(rng *rand.Rand, raw []byte) []byte {
	seqs := [][]byte{
		{0xff},
		{0xc0, 0x80},
		{0xe0, 0x80, 0x80},
		{0xed, 0xa0, 0x80},
		{0xf4, 0x90, 0x80, 0x80},
		{0x01},
		{0x09},
		{0x1f},
	}
	var quotes []int
	inStr := false
	escaped := false
	for i := range raw {
		b := raw[i]
		if !inStr {
			if b == '"' {
				inStr = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '"' {
			quotes = append(quotes, i)
			inStr = false
		}
	}
	seq := seqs[rng.IntN(len(seqs))]
	if len(quotes) == 0 {
		return append(seq, raw...)
	}
	pos := quotes[rng.IntN(len(quotes))]
	var out bytes.Buffer
	out.Write(raw[:pos])
	out.Write(seq)
	out.Write(raw[pos:])
	if utf8.Valid(out.Bytes()) && seq[0] >= 0x80 {
		out.WriteByte(0xff)
	}
	return out.Bytes()
}

func truncateJSONBytes(rng *rand.Rand, raw []byte) []byte {
	if len(raw) <= 1 {
		return nil
	}
	cut := 1 + rng.IntN(len(raw)-1)
	return append([]byte{}, raw[:cut]...)
}

func appendTrailingGarbage(rng *rand.Rand, raw []byte) []byte {
	tails := []string{" 0", " {}", " ,", " ]", " null", " \"extra\"", "\xef\xbb\xbf"}
	var out bytes.Buffer
	out.Write(raw)
	out.WriteString(tails[rng.IntN(len(tails))])
	return out.Bytes()
}

func corruptJSONDelimiter(rng *rand.Rand, raw []byte) []byte {
	var positions []int
	inStr := false
	escaped := false
	for i := range raw {
		b := raw[i]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			switch b {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		if b == '"' {
			inStr = true
			continue
		}
		if b == ':' || b == ',' || b == '{' || b == '}' || b == '[' || b == ']' {
			positions = append(positions, i)
		}
	}
	if len(positions) == 0 {
		return append([]byte("{"), raw...)
	}
	pos := positions[rng.IntN(len(positions))]
	swaps := []byte{',', ':', '{', '}', '[', ']'}
	rep := swaps[rng.IntN(len(swaps))]
	if rep == raw[pos] {
		rep = ','
		if raw[pos] == ',' {
			rep = ':'
		}
	}
	out := append([]byte{}, raw...)
	out[pos] = rep
	return out
}

func injectReservedAmpersandKey(rng *rand.Rand, raw []byte) []byte {
	idx := bytes.IndexByte(raw, '{')
	if idx < 0 {
		return raw
	}
	variants := []string{
		`"__pre":1,"&node":1,`,
		`"&node_extra":1,`,
		`"\u0026reserved":1,`,
	}
	ins := variants[rng.IntN(len(variants))]
	var out bytes.Buffer
	out.Write(raw[:idx+1])
	out.WriteString(ins)
	if idx+1 < len(raw) && raw[idx+1] == '}' {
		// trim trailing comma for empty `{}`
		b := out.Bytes()
		out.Truncate(len(b) - 1)
	}
	out.Write(raw[idx+1:])
	return out.Bytes()
}
