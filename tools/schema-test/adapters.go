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
// exist before it runs for a language; Cmd is its command line for one.
type harness struct {
	Name  string
	Build func(lang string) [][]string
	Cmd   func(lang, work string) []string
}

// alignArrays is test/verify-bitphase --adapter: the align-arrays family's
// driver for every leg, reading its rows from the requests (alignarrays.go).
var alignArrays = harness{
	Name:  "align-arrays",
	Build: func(string) [][]string { return [][]string{{"make", "bin/schema", "build/verify-bitphase-bin"}} },
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
// (make conformance-leg-<lang>), runs the leg over every surface as
// `harness run --only <lang> --claims` scores it, and answers each requested
// case from that run.
var conformance = harness{
	Name: "conformance",
	Build: func(lang string) [][]string {
		return [][]string{{"make", "conformance-leg-" + lang}}
	},
	Cmd: func(lang, work string) []string {
		return []string{"./build/conformance-harness-vm", "adapter", "--lang", lang, "--work", work}
	},
}

// harnessFor is the harness that runs a family, if one does. Which languages
// answer a family is the registry's (its owed lines), never the harness's.
func harnessFor(family string) (harness, bool) {
	switch {
	case family == alignArrays.Name:
		return alignArrays, true
	case strings.HasPrefix(family, conformanceFamilyPrefix):
		return conformance, true
	}
	return harness{}, false
}

// adapters are the adapters a language row may name: the families each
// language has a harness wired for. A language row that names none is not
// ready, and every one of its instances is MISSING until it does.
//
// The language-free kinds have one adapter each: gotest runs a Go test by
// name, plant runs a negative control (a named test against a broken build,
// which must come out red), model-check runs a TLC configuration held to its
// recorded run. docs/TEST-REGISTRY.md lists the kinds still to come
// (goldens, lint gates, trace checks, benches) and the contract each will
// have.
func adapters(log io.Writer) map[string]testreg.Adapter {
	b := &builder{log: log, done: map[string]error{}}
	return map[string]testreg.Adapter{
		// every language row names corpus: one adapter routes each family
		// to its harness for any language, and what a language does not
		// answer yet is an owed line of the registry, MISSING by name
		"corpus":      &harnessAdapter{build: b, log: log},
		"gotest":      newGoTestAdapter(log, ".", goTestJobs()),
		"model-check": modelCheckAdapter{build: b, log: log},
	}
}

// goTestJobs is how many Go packages the gotest adapter runs at once: half
// the machine, since each package's own tests run in parallel too.
func goTestJobs() int { return min(8, max(1, runtime.NumCPU()/2)) }

// withPlant adds the plant adapter, which runs its targets through rn.
func withPlant(ads map[string]testreg.Adapter, rn *testreg.Runner) {
	ads["plant"] = plantAdapter{rn: rn}
}

// builder runs each build step once per process, however many languages ask.
type builder struct {
	mu   sync.Mutex
	log  io.Writer
	done map[string]error
}

func (b *builder) run(ctx context.Context, argv []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.Join(argv, "\x00")
	if err, ok := b.done[key]; ok {
		return err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	bound(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("build step %q: %w\n%s", strings.Join(argv, " "), err, tail(out.String(), 40))
	}
	b.done[key] = err
	return err
}

// workRoot holds every harness call's work folder.
const workRoot = "build/schema-test-work"

// harnessAdapter runs a language's instances through each family's harness.
type harnessAdapter struct {
	build *builder
	log   io.Writer
}

func (a *harnessAdapter) Answers(t testreg.Test) bool {
	_, ok := harnessFor(t.Family)
	return ok && t.PerLang()
}

func (a *harnessAdapter) Run(ctx context.Context, lang string, instances []testreg.Instance) ([]testreg.Result, error) {
	// one call per harness: the conformance harness scores every surface
	// of a leg in one run, so its families go to it together
	byHarness := map[string][]testreg.Instance{}
	var names []string
	for _, inst := range instances {
		h, ok := harnessFor(inst.Test.Family)
		if !ok {
			return nil, fmt.Errorf("%s: no harness for family %s", inst.Full, inst.Test.Family)
		}
		if _, seen := byHarness[h.Name]; !seen {
			names = append(names, h.Name)
		}
		byHarness[h.Name] = append(byHarness[h.Name], inst)
	}
	var all []testreg.Result
	for _, name := range names {
		h, _ := harnessFor(byHarness[name][0].Test.Family)
		fam := h.Name
		for _, step := range h.Build(lang) {
			if err := a.build.run(ctx, step); err != nil {
				return all, err
			}
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
