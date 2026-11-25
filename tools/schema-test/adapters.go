package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// harness is a program that runs one family's instances for any language it
// is given: it reads request lines on its standard input and writes one
// result line per request (internal/testreg's protocol). Build is what has to
// exist before it runs; Cmd is its command line for a language.
type harness struct {
	Build [][]string
	Cmd   func(lang, work string) []string
}

// harnesses are the families a harness exists for, by family. Which
// languages answer a family is the adapters' list below, never the harness's.
var harnesses = map[string]harness{
	// test/verify-bitphase --adapter: the align-arrays family's driver for
	// every leg, reading its rows from the requests (alignarrays.go).
	"align-arrays": {
		Build: [][]string{{"make", "bin/schema", "build/verify-bitphase-bin"}},
		Cmd: func(lang, work string) []string {
			return []string{"./build/verify-bitphase-bin", "--adapter", "--lang", lang,
				"--schema", "./bin/schema", "--schema-moo", "./build/schema-moo",
				"--work", work}
		},
	},
}

// adapters are the adapters a language row may name: the families each
// language has a harness wired for. A language row that names none is not
// ready, and every one of its instances is MISSING until it does.
//
// The language-free kinds have one adapter each: gotest runs a Go test by
// name, plant runs a negative control (a named test against a broken build,
// which must come out red). docs/TEST-REGISTRY.md lists the kinds still to
// come (goldens, lint gates, TLC runs and trace checks, benches) and the
// contract each will have.
func adapters(log io.Writer) map[string]testreg.Adapter {
	b := &builder{log: log, done: map[string]error{}}
	return map[string]testreg.Adapter{
		"go":     &harnessAdapter{families: []string{"align-arrays"}, build: b, log: log},
		"lua":    &harnessAdapter{families: []string{"align-arrays"}, build: b, log: log},
		"gotest": newGoTestAdapter(log, ".", goTestJobs()),
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
	families []string
	build    *builder
	log      io.Writer
}

func (a *harnessAdapter) Answers(t testreg.Test) bool {
	_, ok := harnesses[t.Family]
	return ok && t.PerLang() && slices.Contains(a.families, t.Family)
}

func (a *harnessAdapter) Run(ctx context.Context, lang string, instances []testreg.Instance) ([]testreg.Result, error) {
	byFamily := map[string][]testreg.Instance{}
	var families []string
	for _, inst := range instances {
		if _, ok := byFamily[inst.Test.Family]; !ok {
			families = append(families, inst.Test.Family)
		}
		byFamily[inst.Test.Family] = append(byFamily[inst.Test.Family], inst)
	}
	var all []testreg.Result
	for _, fam := range families {
		h, ok := harnesses[fam]
		if !ok {
			return all, fmt.Errorf("no harness for family %s", fam)
		}
		for _, step := range h.Build {
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
		res, err := runHarness(ctx, h.Cmd(lang, work), byFamily[fam], a.log)
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
