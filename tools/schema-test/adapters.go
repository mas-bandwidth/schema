package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// harness is a program that runs one family's instances for any language it
// is given: it reads request lines on its standard input and writes one
// result line per request (internal/testreg's protocol). Build is what has to
// exist before it runs for a language; Cmd is its command line for one. A
// harness with Run answers its instances itself instead of through Cmd
// (nameBuild: Go subtests run through make), after its Build.
type harness struct {
	Name  string
	Build func(lang string) [][]string
	Cmd   func(lang, work string) []string
	Run   func(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error)
}

// alignArrays is test/verify-bitphase --adapter: the align-arrays family's
// driver for every leg, reading its rows from the requests (alignarrays.go).
// Its build is the leg's own: make verify-align-arrays-deps-<lang>
// (make/verify-widen-18.mk) builds the driver, bin/schema and what
// VERIFY_WIDEN_DEPS_<lang> names, build/schema-moo for moo among them, so
// every path Cmd names exists before it runs.
var alignArrays = harness{
	Name: "align-arrays",
	Build: func(lang string) [][]string {
		return [][]string{{"make", "verify-align-arrays-deps-" + lang}}
	},
	Cmd: func(lang, work string) []string {
		return []string{"./build/verify-bitphase-bin", "--adapter", "--lang", lang,
			"--schema", "./bin/schema", "--schema-moo", "./build/schema-moo",
			"--work", work}
	},
}

// conformanceFamilyPrefix starts every family of the tables conformance
// corpus: conformance-<surface>, one test per case of the surface.
const conformanceFamilyPrefix = "conformance-"

// conformance is the tables conformance harness's adapter mode
// (test/conformance/harness/adapter.go): it builds the one leg's driver
// (make conformance-leg-<lang>), runs the leg over the requested cases and
// the cases their verdicts depend on (test/conformance/harness/scope.go) as
// `harness run --only <lang> --claims` scores them, and answers each
// requested case from that run.
var conformance = harness{
	Name: "conformance",
	Build: func(lang string) [][]string {
		return [][]string{{"make", "conformance-leg-" + lang}}
	},
	Cmd: func(lang, work string) []string {
		return []string{"./build/conformance-harness-vm", "adapter", "--lang", lang, "--work", work}
	},
}

// wireFuzzHarness is the wire fuzz's (make/wire-fuzz-rows.mk): make
// wire-fuzz-deps-<lang> builds the leg's wire-fuzz driver, and make
// wire-fuzz-adapter-<lang> answers the families wire-fuzz-vector (every
// pinned vector, replayed exactly) and wire-fuzz (every arm at the run's seed)
// in every configuration of the leg (test/conformance/harness/wirefuzz_registry.go).
var wireFuzzHarness = harness{
	Name:  "wire-fuzz",
	Build: func(lang string) [][]string { return [][]string{{"make", "wire-fuzz-deps-" + lang}} },
	Cmd: func(lang, work string) []string {
		return []string{"make", "-s", "wire-fuzz-adapter-" + lang, "WFZ_WORK=" + work}
	},
}

// wireFuzzFamilies are the families the wire-fuzz harness answers.
var wireFuzzFamilies = map[string]bool{"wire-fuzz": true, "wire-fuzz-vector": true}

// bitPhaseFamily is verify-bitphase's main sweep (test/verify-bitphase,
// bitphase_rows.go), which the same --adapter answers.
const bitPhaseFamily = "bit-phase"

// bomEntry is the BOM entry gate's adapter (test/conformance/bom-entry
// --adapter): the harness's adapter mode over
// testdata/conformance/tables/bom-entry/MANIFEST.txt, the leg listing its
// json-hostile surface alone. Its rows are the bom-entry family
// (test/conformance/harness/adapter.go, bomFamily).
var bomEntry = harness{
	Name: "bom-entry",
	Build: func(lang string) [][]string {
		return [][]string{{"make", "conformance-leg-" + lang}}
	},
	Cmd: func(lang, work string) []string {
		return []string{"sh", "test/conformance/bom-entry", "--adapter", lang, work}
	},
}

// harnessFor is the harness that runs a test, if one does: a union row's
// (its inputs name each language's Go test) is goUnion, and every other
// row's is its family's. Which languages answer a family is the registry's
// (its lang rows and unrepresentable declarations), never the harness's.
func harnessFor(t testreg.Test) (harness, bool) {
	family := t.Family
	switch {
	case isUnion(t):
		return goUnion, true
	case family == textformFamily:
		return textformHarness, true
	case family == csRowsFamily:
		return csRowsHarness, true
	case family == gooseFamily:
		return gooseHarness, true
	case family == luauVecFamily:
		return luauHarness, true
	case family == alignArrays.Name || family == bitPhaseFamily:
		return alignArrays, true
	case strings.HasPrefix(family, conformanceFamilyPrefix):
		return conformance, true
	case family == bomEntry.Name:
		return bomEntry, true
	case family == featureWitnessFamily:
		return featureWitness, true
	case family == nameBuild.Name:
		return nameBuild, true
	case wireFuzzFamilies[family]:
		return wireFuzzHarness, true
	case family == tablesHarness.Name:
		return tablesHarness, true
	case family == jsontestFamily:
		return jsontestHarness, true
	}
	return hostileHarnessFor(family)
}

// adapters are the adapters a language row may name: the families each
// language has a harness wired for. A language row that names none is not
// ready, and every one of its instances is MISSING until it does.
//
// The language-free kinds have one adapter each: gotest runs a Go test by
// name, make runs a make gate by target, golden checks a pinned output
// against its writer, plant runs a negative control (a named test against a
// broken build, which must come out red), model-check runs a TLC
// configuration held to its recorded run. docs/TEST-REGISTRY.md lists the
// kinds still to come (lint gates, trace checks, benches) and the contract
// each will have.
func adapters(log io.Writer) map[string]testreg.Adapter {
	b := &builder{log: log, done: map[string]error{}}
	return map[string]testreg.Adapter{
		// every language row names corpus: one adapter routes each family
		// to its harness for any language, and a case a language does not
		// answer yet runs and is red
		"corpus":      newHarnessAdapter(b, log),
		"gotest":      newGoTestAdapterBuilding(log, ".", goTestJobs(), b.run),
		"make":        newMakeAdapter(),
		"make-gotest": newMakeGoTestAdapter("."),
		"golden":      newGoldenAdapter(),
		"model-check": modelCheckAdapter{build: b, log: log},
	}
}

// withRunner adds the plant adapter, which runs its targets through rn, and
// hands the golden adapter the registry rn loaded.
func withRunner(ads map[string]testreg.Adapter, rn *testreg.Runner) {
	ads["plant"] = newPlantAdapter(rn)
	if g, ok := ads["golden"].(goldenAdapter); ok {
		g.reg = rn.Reg
		ads["golden"] = g
	}
	if m, ok := ads["make-gotest"].(makeGoTestAdapter); ok {
		m.reg = rn.Reg
		ads["make-gotest"] = m
	}
}

// goTestJobs is how many Go packages the gotest adapter runs at once: half
// the machine, since each package's own tests run in parallel too.
func goTestJobs() int { return min(8, max(1, runtime.NumCPU()/2)) }

// builder runs each build step once per process, however many languages ask.
type builder struct {
	mu   sync.Mutex
	log  io.Writer
	done map[string]error
	// step runs one build command and returns its combined output; nil
	// runs it (execStep), a unit test hands in its own
	step func(ctx context.Context, argv []string) ([]byte, error)
}

// execStep runs one build command, bounded, for its combined output.
func execStep(ctx context.Context, argv []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	bound(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}

func (b *builder) run(ctx context.Context, argv []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// a step already run answers at once: it touches nothing, so it never
	// waits for the checkout another make holds
	key := strings.Join(argv, "\x00")
	if err, ok := b.done[key]; ok {
		return err
	}
	release, err := holdCheckout(ctx)
	if err != nil {
		return fmt.Errorf("build step %q: %w", strings.Join(argv, " "), err)
	}
	defer release()
	step := b.step
	if step == nil {
		step = execStep
	}
	out, err := step(ctx, argv)
	if err != nil {
		err = fmt.Errorf("build step %q: %w\n%s", strings.Join(argv, " "), err, tail(string(out), 40))
	}
	// a step its deadline cut short gave no answer: it is never recorded as
	// a build that failed, and the next call to ask for it runs it again
	if ctx.Err() == nil {
		b.done[key] = err
	}
	return err
}

// workRoot holds every harness call's work folder.
const workRoot = "build/schema-test-work"

// harnessAdapter runs a language's instances through each family's harness.
type harnessAdapter struct {
	build *builder
	log   io.Writer
	// mk runs a per-language row's make gates (langmake.go); controls runs
	// the ones that are negative controls, with no -j as their job ran them
	mk, controls makeAdapter
}

func newHarnessAdapter(b *builder, log io.Writer) *harnessAdapter {
	controls := newMakeAdapter()
	controls.jobs = 0
	return &harnessAdapter{build: b, log: log, mk: newMakeAdapter(), controls: controls}
}

// Serial: the harness takes the checkout for its whole run (runHarness), so
// it runs between the other checkout-taking groups, never waiting for one
// inside its own deadline (testreg.Serial).
func (a *harnessAdapter) Serial() bool { return true }

func (a *harnessAdapter) Answers(t testreg.Test) bool {
	if _, has, err := langMakeGates(t); has {
		return err == nil
	}
	_, ok := harnessFor(t)
	return ok && t.PerLang()
}

// Prepare runs each harness's build steps for the language once, before any
// call's deadline starts (testreg.Preparer): a leg built cold is the run's
// cost, and Run's own build steps then find it built.
func (a *harnessAdapter) Prepare(ctx context.Context, lang string, instances []testreg.Instance) error {
	seen := map[string]bool{}
	for _, inst := range instances {
		h, ok := harnessFor(inst.Test)
		if !ok || seen[h.Name] {
			continue // Run fails a family with no harness, naming it
		}
		seen[h.Name] = true
		for _, step := range h.Build(lang) {
			if err := a.build.run(ctx, step); err != nil {
				return err
			}
		}
		// the builds the union rows' Go tests read (goneeds.go); one that
		// fails fails the rows that read it, in Run
		if h.Name == goUnion.Name {
			if _, err := newGoTestAdapterBuilding(a.log, ".", 1, a.build.run).buildNeeds(ctx, unionRefs(lang, instances)); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err // the deadline's, never a build failure
			}
		}
	}
	return nil
}

func (a *harnessAdapter) Run(ctx context.Context, lang string, instances []testreg.Instance) ([]testreg.Result, error) {
	ctx = withBuilder(ctx, a.build)
	// one call per harness: the conformance harness scores every surface
	// of a leg in one run, so its families go to it together
	byHarness := map[string][]testreg.Instance{}
	var names []string
	var gated []testreg.Instance
	for _, inst := range instances {
		if _, has, _ := langMakeGates(inst.Test); has {
			gated = append(gated, inst)
			continue
		}
		h, ok := harnessFor(inst.Test)
		if !ok {
			return nil, fmt.Errorf("%s: no harness for family %s", inst.Full, inst.Test.Family)
		}
		if _, seen := byHarness[h.Name]; !seen {
			names = append(names, h.Name)
		}
		byHarness[h.Name] = append(byHarness[h.Name], inst)
	}
	var all []testreg.Result
	if len(gated) > 0 {
		res, err := runLangMake(ctx, a.mk, a.controls, lang, gated)
		all = append(all, res...)
		if err != nil {
			return all, err
		}
	}
	for _, name := range names {
		h, _ := harnessFor(byHarness[name][0].Test)
		fam := h.Name
		for _, step := range h.Build(lang) {
			if err := a.build.run(ctx, step); err != nil {
				return all, err
			}
		}
		if h.Run != nil {
			res, err := h.Run(ctx, lang, byHarness[name], a.log)
			all = append(all, res...)
			if err != nil {
				return all, fmt.Errorf("%s harness for %s: %w", fam, lang, err)
			}
			continue
		}
		// each call has its own work folder: a plant runs its target while
		// the same language's own instances may be running
		if err := os.MkdirAll(workRoot, 0o755); err != nil {
			return all, err
		}
		work, err := os.MkdirTemp(workRoot, fam+"-"+lang+"-")
		if err != nil {
			return all, err
		}
		res, err := runHarness(ctx, h.Cmd(lang, work), byHarness[name], a.log)
		if rmErr := os.RemoveAll(work); rmErr != nil && err == nil {
			err = rmErr
		}
		all = append(all, res...)
		if err != nil {
			return all, fmt.Errorf("%s harness for %s: %w", fam, lang, err)
		}
	}
	return all, nil
}

// runHarness starts the harness, hands it the requests and reads its results.
// Its own log lines go to log; a non-zero exit is an error after whatever
// results it did write.
func runHarness(ctx context.Context, argv []string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	// the harness runs bin/schema and what the build steps made: no make
	// in the checkout relinks them under it
	release, err := holdCheckout(ctx)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", strings.Join(argv, " "), err)
	}
	defer release()
	var in bytes.Buffer
	if err := testreg.EncodeRequests(&in, instances); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	bound(cmd)
	if env := testreg.EnvFrom(ctx); len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Stdin = &in
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	runErr := cmd.Run()
	results, lines, parseErr := testreg.ParseResults(&out)
	if len(lines) > 0 {
		if _, err := io.WriteString(log, strings.Join(lines, "\n")+"\n"); err != nil {
			return results, err
		}
	}
	if parseErr == nil {
		parseErr = testreg.RefuseSkips(strings.Split(errOut.String(), "\n"))
	}
	switch {
	case ctx.Err() != nil:
		return results, fmt.Errorf("%q: %w\n%s", strings.Join(argv, " "), ctx.Err(), tail(errOut.String(), 40))
	case runErr != nil:
		return results, fmt.Errorf("%q: %w\n%s", strings.Join(argv, " "), runErr, tail(errOut.String(), 40))
	case parseErr != nil:
		return results, parseErr
	}
	if errOut.Len() > 0 {
		if _, err := io.Copy(log, &errOut); err != nil {
			return results, err
		}
	}
	return results, nil
}

// bound kills the child with its process group when the context ends, and
// gives its pipes a moment to drain after it does.
func bound(cmd *exec.Cmd) {
	setGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("... (%d lines before)", len(lines)-n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
