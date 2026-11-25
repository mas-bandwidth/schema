package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// Card A4 (docs/TEST-REGISTRY.md, corpus adapter): the conformance corpora
// that lived only in test/conformance/{textform,cs,luau,packet,goose} as
// data and drivers. Each behavioural case is one per-language registry row
// on the corpus adapter; a language with no runner for the case is red by
// name, never left out. Packet's data is claimed by its existing gotest
// rows (their inputs name the files), so it needs no family here.
//
//   textform     test/conformance/textform positives and refusals, run by
//                each language's own Go test for the corpus (subtests
//                where the test has them, the whole test otherwise)
//   cs-rows      test/conformance/cs/rows programs plus storage-debug,
//                run by tools/conformancerows, which prints one PASS or
//                FAIL line per row; other rows legs answer a case when
//                their directory holds a row of that name
//   luau-vectors the Luau leg's pinned wire-fuzz vectors, answered by the
//                wire-fuzz harness like every pinned vector
//   goose-leg    the Goose leg's units glue and write-refusal packet
//                probe, run by the leg's own gates

const (
	textformFamily = "textform"
	csRowsFamily   = "cs-rows"
	gooseFamily    = "goose-leg"
	luauVecFamily  = "luau-vectors"
)

var a4Families = map[string]bool{
	textformFamily: true, csRowsFamily: true, gooseFamily: true, luauVecFamily: true,
}

// a4Inputs is a textform, cs-rows or goose-leg row's inputs: the corpus
// case it answers, the committed files it reads (the registry indexes
// them where they are), and the driver that answers it, if the case is
// a driver of its own.
type a4Inputs struct {
	Case    string   `json:"case"`
	Refusal bool     `json:"refusal,omitempty"`
	Files   []string `json:"files,omitempty"`
	Driver  string   `json:"driver,omitempty"`
}

func a4Row(typ, family, slug string, in a4Inputs, checks, expect, source string) (testreg.Test, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return testreg.Test{}, err
	}
	return testreg.Test{
		Key: typ + "." + family + "." + slug, Type: typ, Family: family, Case: slug,
		Tier: "functional", Checks: checks, Inputs: raw, Expect: expect, Source: source,
	}, nil
}

// a4Rows is every registry row of the A4 families, in family then key order.
func a4Rows(root string) ([]testreg.Test, error) {
	var out []testreg.Test
	for _, rows := range []func(string) ([]testreg.Test, error){textformRows, csRowsRows, gooseRows, luauVectorRows} {
		rs, err := rows(root)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	slices.SortFunc(out, func(a, b testreg.Test) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

var a4SlugDrop = regexp.MustCompile(`[^a-z0-9]+`)

// a4CaseSlug is a corpus name as a registry case: lowercase, every run
// of other characters one hyphen (the harness's rule for corpus rows).
func a4CaseSlug(name string) string {
	return strings.Trim(a4SlugDrop.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// luauInputs is a Luau vector row's inputs, in the wire-fuzz harness's
// shape: the vector, its unit and root, and the files it reads.
type luauInputs struct {
	Vector string   `json:"vector,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Root   string   `json:"root,omitempty"`
	Files  []string `json:"files,omitempty"`
}

// ---- textform ----

func textformRows(root string) ([]testreg.Test, error) {
	dir := filepath.Join(root, "test/conformance/textform")
	contract := "test/conformance/textform/contract.json"
	if _, err := os.Stat(filepath.Join(dir, "contract.json")); err != nil {
		return nil, err
	}
	var out []testreg.Test
	positives := []string{"roadmap", "layout", "kinds", "variable", "unbounded"}
	for _, stem := range positives {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		var files []string
		for _, e := range ents {
			if !e.IsDir() && strings.HasPrefix(e.Name(), stem+".") {
				files = append(files, "test/conformance/textform/"+e.Name())
			}
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("textform: positive %s has no files", stem)
		}
		files = append(files, contract)
		if stem == "variable" {
			files = append(files, "test/conformance/textform/variable-contract.json")
		}
		slices.Sort(files)
		row, err := a4Row("roundtrip", textformFamily, stem, a4Inputs{Case: stem, Files: files},
			"textform "+stem+": the input reads to its typed-value oracle and rewrites byte for byte",
			"the leg's value equals the .value.json oracle (with the contract defaults) and its rewrite equals the .rewrite.sexp bytes",
			files[0]+":1")
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	refDir := filepath.Join(dir, "refusals")
	names, err := filepath.Glob(filepath.Join(refDir, "*.sexp"))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("textform: no refusal vectors")
	}
	for _, name := range names {
		stem := strings.TrimSuffix(filepath.Base(name), ".sexp")
		reason := filepath.Join(refDir, stem+".reason")
		if _, err := os.Stat(reason); err != nil {
			return nil, fmt.Errorf("textform: refusal %s: %w", stem, err)
		}
		files := []string{"test/conformance/textform/refusals/" + stem + ".sexp", "test/conformance/textform/refusals/" + stem + ".reason"}
		row, err := a4Row("load-refusal", textformFamily, a4CaseSlug(stem), a4Inputs{Case: stem, Refusal: true, Files: files},
			"textform refusal "+stem+": the input is refused by the stable name its .reason names",
			"refused on load, by the .reason name (verdict only); no document or value is returned",
			files[0]+":1")
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// textformRunner is one language's Go test for one textform case: a
// subtest when the test names the case, the whole test otherwise.
type textformRunner struct{ pkg, test, sub string }

func textformRunnerFor(lang string, in a4Inputs) (textformRunner, bool) {
	graph := in.Case == "duplicate-map" || in.Case == "forward-reference"
	switch lang {
	case "go":
		// The corpus runs inside the generated units TestCommentedTextForm
		// builds (its probe holds the per-vector tests), on fixed and
		// variable roots alike: one verdict for the corpus in Go.
		if in.Case == "unbounded" && !in.Refusal {
			return textformRunner{"./internal/codegen/gotable", "TestTextFormUnboundedTextListsAndMapKeys", ""}, true
		}
		return textformRunner{"./internal/codegen/gotable", "TestCommentedTextForm", ""}, true
	case "cpp":
		if in.Refusal {
			if graph {
				return textformRunner{}, false
			}
			return textformRunner{"./compiler", "TestCPPTextFormDocumentVectors", in.Case}, true
		}
		switch in.Case {
		case "roadmap", "layout", "kinds":
			return textformRunner{"./compiler", "TestCPPTextFormDocumentVectors", in.Case}, true
		case "variable":
			return textformRunner{"./compiler", "TestCPPTextFormDocumentEdits", "variable"}, true
		}
	case "python":
		if in.Case == "unbounded" && !in.Refusal {
			return textformRunner{"./internal/codegen/pythontable", "TestPythonHandtextUnbounded", ""}, true
		}
		return textformRunner{"./internal/codegen/pythontable", "TestPythonHandtextCorpus", ""}, true
	case "squirrel":
		if in.Case == "variable" || in.Case == "unbounded" {
			return textformRunner{}, false
		}
		return textformRunner{"./internal/codegen/squirreltable", "TestSquirrelDocumentVectors", ""}, true
	}
	return textformRunner{}, false
}

var textformHarness = harness{
	Name:  textformFamily,
	Build: func(string) [][]string { return nil },
	Run:   runTextform,
}

func runTextform(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	return runGoCaseGroups(ctx, lang, instances, log, func(lang string, inst testreg.Instance) (textformRunner, bool) {
		var in a4Inputs
		if err := json.Unmarshal(inst.Test.Inputs, &in); err != nil {
			return textformRunner{}, false
		}
		return textformRunnerFor(lang, in)
	}, "textform")
}

// runGoCaseGroups runs instances whose runner is a Go test: one go test
// call per (package, test), selecting exactly the asked subtests, and
// one verdict per instance. A language with no runner for a case fails
// that instance by name without running anything.
func runGoCaseGroups(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer,
	runner func(string, testreg.Instance) (textformRunner, bool), what string) ([]testreg.Result, error) {
	var out []testreg.Result
	type group struct {
		r    textformRunner
		subs map[string][]string // subtest (or test for whole) -> its instances
	}
	groups := map[string]*group{}
	var order []string
	candidates := []string{"go", "cpp", "python", "squirrel"}
	for _, inst := range instances {
		r, ok := runner(lang, inst)
		if !ok {
			var answered []string
			for _, l := range candidates {
				if _, yes := runner(l, inst); yes {
					answered = append(answered, l)
				}
			}
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("%s has no Go test for %s: the case is unanswered in %s (answered in %s)", lang, inst.Test.Key, lang, strings.Join(answered, ", "))})
			continue
		}
		key := r.pkg + "\x00" + r.test
		g, seen := groups[key]
		if !seen {
			g = &group{r: r, subs: map[string][]string{}}
			groups[key] = g
			order = append(order, key)
		}
		name := r.test
		if r.sub != "" {
			name = r.test + "/" + r.sub
		}
		g.subs[name] = append(g.subs[name], inst.Full)
	}
	for _, key := range order {
		g := groups[key]
		res, err := runOneGoGroup(ctx, g.r, g.subs, log)
		out = append(out, res...)
		if err != nil {
			return out, fmt.Errorf("%s %s: %w", what, lang, err)
		}
	}
	return out, nil
}

// runOneGoGroup runs one package's test once for the wanted names.
func runOneGoGroup(ctx context.Context, r textformRunner, want map[string][]string, log io.Writer) ([]testreg.Result, error) {
	var subs []string
	whole := false
	for name := range want {
		if name == r.test {
			whole = true
		} else {
			subs = append(subs, strings.TrimPrefix(name, r.test+"/"))
		}
	}
	pattern := "^" + regexp.QuoteMeta(r.test) + "$"
	if !whole && len(subs) > 0 {
		slices.Sort(subs)
		quoted := make([]string, len(subs))
		for i, s := range subs {
			quoted[i] = regexp.QuoteMeta(s)
		}
		pattern = "^" + regexp.QuoteMeta(r.test) + "$/^(" + strings.Join(quoted, "|") + ")$"
	}
	args := []string{"test", "-count=1", "-json", "-timeout", goTestTimeout(ctx, time.Now()), "-run", pattern, r.pkg}
	cmd := exec.CommandContext(ctx, "go", args...)
	bound(cmd)
	cmd.Env = os.Environ()
	for k, v := range testreg.EnvFrom(ctx) {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if stderr.Len() > 0 {
		if _, err := io.WriteString(log, stderr.String()); err != nil {
			return nil, err
		}
	}
	res, other, err := goCaseResults(&stdout, want)
	if err != nil {
		return res, err
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("go test %s %s: %w", r.pkg, r.test, ctx.Err())
	}
	if len(res) == 0 {
		return res, fmt.Errorf("go test %s %s answered none of the %d asked (%s matched no test): %v\n%s", r.pkg, r.test, len(want), pattern, runErr, tail(strings.Join(other, "\n"), 20))
	}
	return res, nil
}

// goCaseResults reads go test -json events for the wanted full names
// (a top-level test or one of its subtests). A wanted name with no pass
// or fail event is left out, so the driver reports it as not reported.
func goCaseResults(rd io.Reader, want map[string][]string) (out []testreg.Result, other []string, err error) {
	type event struct{ Action, Test, Output string }
	output := map[string][]string{}
	dec := json.NewDecoder(rd)
	for {
		var ev event
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			return out, other, fmt.Errorf("go test -json: %w", err)
		}
		insts, asked := want[ev.Test]
		if !asked {
			if (ev.Action == "output" || ev.Action == "build-output") && ev.Output != "" {
				other = append(other, strings.TrimRight(ev.Output, "\n"))
			}
			continue
		}
		for _, inst := range insts {
			switch ev.Action {
			case "output":
				output[ev.Test] = append(output[ev.Test], strings.TrimRight(ev.Output, "\n"))
			case "pass":
				out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass})
			case "fail":
				out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: tail(strings.Join(output[ev.Test], "\n"), 20)})
			case "skip":
				out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[ev.Test], "\n"), 5)})
			}
		}
	}
	return out, other, nil
}

// ---- cs-rows ----

func csRowsRows(root string) ([]testreg.Test, error) {
	rowsDir := filepath.Join(root, "test/conformance/cs/rows")
	ents, err := os.ReadDir(rowsDir)
	if err != nil {
		return nil, err
	}
	var out []testreg.Test
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		files, err := dirFiles(root, filepath.Join(rowsDir, name))
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("cs-rows: row %s has no files", name)
		}
		if name == "C12" {
			files = append(files, "test/conformance/cs/ci.json", "test/conformance/cs/driver",
				"test/conformance/cs/negative-control", "test/conformance/cs/write-refusal-control")
			slices.Sort(files)
		}
		driver := ""
		for _, f := range files {
			if strings.HasSuffix(f, ".csproj") {
				driver = f
				break
			}
		}
		if driver == "" {
			driver = files[0]
		}
		checks := "C# conformance row " + name + ": its self-contained program (test/conformance/cs/rows/" + name + ") builds against the generated units and passes its law"
		if name == "C12" {
			checks += "; its files also index the C# leg's harness files (ci.json, driver and its two control scripts) where they live"
		}
		row, err := a4Row("roundtrip", csRowsFamily, a4CaseSlug(name), a4Inputs{Case: name, Files: files, Driver: driver},
			checks,
			"the row program exits 0 (tools/conformancerows prints PASS cs/"+name+")",
			files[0]+":1")
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	sdDir := filepath.Join(root, "test/conformance/cs/storage-debug")
	files, err := dirFiles(root, sdDir)
	if err != nil {
		return nil, err
	}
	row, err := a4Row("write-refusal", csRowsFamily, "storage-debug", a4Inputs{Case: "storage-debug", Files: files, Driver: "test/conformance/cs/storage-debug/StorageDebug.csproj"},
		"C# storage-debug: the debug storage writer asserts each bad write it is handed (make write-refusal-storage-debug-cs)",
		"every bad write asserts in the debug build; the gate exits 0",
		"test/conformance/cs/storage-debug/WriteChecks.schema:1")
	if err != nil {
		return nil, err
	}
	out = append(out, row)
	if len(out) == 0 {
		return nil, fmt.Errorf("cs-rows: no rows")
	}
	return out, nil
}

func dirFiles(root, dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		rel, err := filepath.Rel(root, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.ToSlash(rel))
	}
	slices.Sort(out)
	return out, nil
}

var csRowsHarness = harness{
	Name:  csRowsFamily,
	Build: func(string) [][]string { return nil },
	Run:   runCsRows,
}

var csRowLegs = map[string]bool{"c": true, "cpp": true, "cs": true, "dart": true, "elixir": true, "java": true, "js": true, "rust": true}

func runCsRows(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	return runCsRowsWith(ctx, execMake, lang, instances, log)
}

func runCsRowsWith(ctx context.Context, run func(context.Context, []string) (string, error), lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	var out []testreg.Result
	want := map[string]string{} // case -> instance
	var batch []string
	for _, inst := range instances {
		var in a4Inputs
		if err := json.Unmarshal(inst.Test.Inputs, &in); err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		want[in.Case] = inst.Full
		if in.Case == "storage-debug" {
			if lang != "cs" {
				out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
					Reason: lang + " has no storage-debug program for cs-rows.storage-debug: the case is unanswered in " + lang + " (answered in cs)"})
				continue
			}
			text, err := run(ctx, []string{"make", "-s", "write-refusal-storage-debug-cs"})
			if werr := writeLog(log, text); werr != nil {
				return out, werr
			}
			if err != nil {
				out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: "make write-refusal-storage-debug-cs: " + oneLine(tail(text, 10))})
			} else {
				out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
			}
			continue
		}
		batch = append(batch, in.Case)
	}
	if len(batch) == 0 {
		return out, nil
	}
	if !csRowLegs[lang] {
		for _, c := range batch {
			out = append(out, testreg.Result{Instance: want[c], Verdict: testreg.Fail,
				Reason: lang + " has no conformance-rows leg for cs-rows." + a4CaseSlug(c) + ": the case is unanswered in " + lang + " (answered in cs)"})
		}
		return out, nil
	}
	target := "conformance-rows-" + lang
	text, runErr := run(ctx, []string{"make", "-s", target})
	if err := writeLog(log, text); err != nil {
		return out, err
	}
	verdicts, reasons := parseCsRowLines(text, lang)
	for _, c := range batch {
		inst := want[c]
		switch v, ok := verdicts[c]; {
		case ok && v:
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass})
		case ok:
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: reasons[c]})
		default:
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("make %s gave the case no verdict: no PASS or FAIL line names %s/%s (exit: %v)", target, lang, c, runErr)})
		}
	}
	return out, nil
}

// parseCsRowLines reads conformancerows output: PASS <leg>/<row> and
// FAIL <leg>/<row> lines, with the failure's cause on its line.
func parseCsRowLines(text, leg string) (pass map[string]bool, reason map[string]string) {
	pass = map[string]bool{}
	reason = map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "PASS" && f[0] != "FAIL") {
			continue
		}
		l, row, ok := strings.Cut(f[1], "/")
		if !ok || l != leg {
			continue
		}
		if f[0] == "PASS" {
			pass[row] = true
		} else {
			pass[row] = false
			reason[row] = line
		}
	}
	return pass, reason
}

// ---- goose-leg ----

func gooseRows(root string) ([]testreg.Test, error) {
	top := []string{
		"test/conformance/goose/ci.json", "test/conformance/goose/driver", "test/conformance/goose/json-conformance",
		"test/conformance/goose/json-value-negative-control", "test/conformance/goose/negative-control",
		"test/conformance/goose/negative-control-accel", "test/conformance/goose/negative-control-fuzz",
		"test/conformance/goose/walk-negative-control", "test/conformance/goose/wirefuzz.build",
	}
	for _, f := range top {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			return nil, err
		}
	}
	files := append([]string{"test/conformance/goose/units/tabledemo.goose"}, top...)
	slices.Sort(files)
	one, err := a4Row("decode", gooseFamily, "tabledemo", a4Inputs{Case: "tabledemo", Files: files, Driver: "test/conformance/goose/units/tabledemo.goose"},
		"goose tabledemo units glue: the leg's JSON conformance run (test/conformance/goose/json-conformance) answers the tabledemo roots through this glue; its files also index the goose leg's harness files where they live",
		"make tables-goose-json-conformance exits 0: every goose json-read, json-write and json-hostile answer equals the corpus's",
		"test/conformance/goose/units/tabledemo.goose:1")
	if err != nil {
		return nil, err
	}
	two, err := a4Row("write-refusal", gooseFamily, "writerefusalpacket", a4Inputs{Case: "writerefusalpacket", Driver: "test/conformance/goose/writerefusalpacket/probe.goose"},
		"goose write-refusal packet probe: the generated packet writer refuses each bad value the probe hands it",
		"the Go test TestWriteRefusalPacketGoose passes: every bad write is refused",
		"test/conformance/goose/writerefusalpacket/probe.goose:1")
	if err != nil {
		return nil, err
	}
	return []testreg.Test{one, two}, nil
}

var gooseHarness = harness{
	Name:  gooseFamily,
	Build: func(string) [][]string { return nil },
	Run:   runGoose,
}

func runGoose(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	if lang != "goose" {
		var out []testreg.Result
		for _, inst := range instances {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: lang + " has no goose-leg program for " + inst.Test.Key + ": the case is unanswered in " + lang + " (answered in goose)"})
		}
		return out, nil
	}
	var out []testreg.Result
	for _, inst := range instances {
		var in a4Inputs
		if err := json.Unmarshal(inst.Test.Inputs, &in); err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		switch in.Case {
		case "tabledemo":
			text, err := execMake(ctx, []string{"make", "-s", "tables-goose-json-conformance"})
			if werr := writeLog(log, text); werr != nil {
				return out, werr
			}
			if err != nil {
				out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: "make tables-goose-json-conformance: " + oneLine(tail(text, 10))})
			} else {
				out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
			}
		case "writerefusalpacket":
			res, err := runOneGoGroup(ctx, textformRunner{"./test/conformance/goose/writerefusalpacket", "TestWriteRefusalPacketGoose", ""},
				map[string][]string{"TestWriteRefusalPacketGoose": {inst.Full}}, log)
			out = append(out, res...)
			if err != nil {
				return out, err
			}
		default:
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: "goose-leg: no runner for case " + in.Case})
		}
	}
	return out, nil
}

// ---- luau pinned vectors (wire-fuzz harness) ----

func luauVectorRows(root string) ([]testreg.Test, error) {
	index := "test/conformance/luau/wire-fuzz-vectors.txt"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(index)))
	if err != nil {
		return nil, err
	}
	topFiles := []string{
		"test/conformance/luau/ci.json", "test/conformance/luau/cook-write", "test/conformance/luau/driver",
		"test/conformance/luau/messagefuzz-driver", "test/conformance/luau/negative-control", "test/conformance/luau/units-control",
		"test/conformance/luau/wire-fuzz-negative-control", "test/conformance/luau/wire-fuzz-vectors-negative-control",
		"test/conformance/luau/wirefuzz-driver", "test/conformance/luau/write-refusal-control",
	}
	var out []testreg.Test
	seen := map[string]string{}
	n := 0
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) != 4 {
			return nil, fmt.Errorf("%s:%d: a vector is <name> <unit> <root> <file>", index, i+1)
		}
		name, unit, rootName, file := f[0], f[1], f[2], f[3]
		if !strings.HasPrefix(file, "test/conformance/luau/vectors/") {
			continue // a shared-index vector: already a wire-fuzz-vector row
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file))); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", index, i+1, err)
		}
		slug := a4CaseSlug(name)
		if prev, dup := seen[slug]; dup {
			return nil, fmt.Errorf("%s: vectors %s and %s are both case %s", index, prev, name, slug)
		}
		seen[slug] = name
		files := []string{file, index}
		legNote := ""
		if n == 0 {
			files = append(files, topFiles...)
			legNote = "; its files also index the Luau leg's harness files where they live"
		}
		n++
		raw, err := json.Marshal(luauInputs{Vector: name, Unit: unit, Root: rootName, Files: files})
		if err != nil {
			return nil, err
		}
		out = append(out, testreg.Test{
			Key: "fuzz." + luauVecFamily + "." + slug, Type: "fuzz", Family: luauVecFamily, Case: slug, Tier: "functional",
			Checks: fmt.Sprintf("pinned Luau wire-fuzz vector %s: %s, read as a file of %s.%s, gets the oracle's answer%s", name, file, unit, rootName, legNote),
			Inputs: raw, Expect: "the leg's acceptance, report, measure and write-back equal the oracle's on the vector, in every configuration of the leg",
			Source: fmt.Sprintf("%s:%d", index, i+1),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no Luau vector", index)
	}
	return out, nil
}

var luauHarness = harness{
	Name: luauVecFamily,
	Build: func(lang string) [][]string {
		if lang != "luau" {
			return nil
		}
		return [][]string{{"make", "build/conformance-harness", "build/conformance-luau", "build/luau-driver/units.luau", "build/luaufuzzhost/luaufuzzhost"}}
	},
	Run: runLuauVectors,
}

// runLuauVectors replays each asked Luau vector through the Luau leg
// exactly as make tables-luau-wire-fuzz-vectors does (one harness
// --replay, 0 divergences to pass). The vectors live beside the Luau
// leg until every leg answers them, so another language's instance is
// red by name without running anything.
func runLuauVectors(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) (res []testreg.Result, err error) {
	if lang != "luau" {
		for _, inst := range instances {
			res = append(res, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: lang + " has no replay of " + inst.Test.Key + ": the vector lives beside the Luau leg until every leg answers it (answered in luau)"})
		}
		return res, nil
	}
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(workRoot, luauVecFamily+"-"+lang+"-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.RemoveAll(work); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	for _, inst := range instances {
		var in luauInputs
		if err := json.Unmarshal(inst.Test.Inputs, &in); err != nil {
			return res, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		if len(in.Files) == 0 {
			return res, fmt.Errorf("%s: inputs name no vector file", inst.Full)
		}
		cmd := exec.CommandContext(ctx, "./build/conformance-harness", "wire-fuzz",
			"--driver", "test/conformance/luau/wirefuzz-driver",
			"--replay", in.Files[0], "--unit", in.Unit, "--root", in.Root,
			"--failed", filepath.Join(work, "failed.bin"))
		bound(cmd)
		cmd.Env = append(os.Environ(), "LUAUFUZZ_NOTE="+filepath.Join(work, "absent"))
		for k, v := range testreg.EnvFrom(ctx) {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		runErr := cmd.Run()
		if err := writeLog(log, out.String()); err != nil {
			return res, err
		}
		switch {
		case ctx.Err() != nil:
			return res, fmt.Errorf("luau vector replay: %w", ctx.Err())
		case runErr == nil && strings.Contains(out.String(), "0 divergences"):
			res = append(res, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
		default:
			res = append(res, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: "luau wire-fuzz replay of " + in.Vector + ": " + oneLine(tail(out.String(), 5))})
		}
	}
	return res, nil
}

func writeLog(log io.Writer, text string) error {
	if text == "" {
		return nil
	}
	_, err := io.WriteString(log, text)
	return err
}
