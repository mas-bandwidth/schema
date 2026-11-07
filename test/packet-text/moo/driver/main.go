// The moo driver every packet gate's moo leg runs through (docs/PORT-MOO.md,
// THE HARNESS). The moo CLI has no io, so a moo program can read no file and
// no stdin: this driver generates each -schema with internal/codegen/moo into
// one fresh directory, copies the gate's moo main beside the modules, writes
// the inputs as the module inputs.moo (moorun.InputsModule: each -input file,
// and stdin as the buffer "stdin"), runs the pinned moo there and relays its
// stdout, stderr and verdict. A gate's harness speaks to it exactly as it
// speaks to a JavaScript or Go driver: one result line per input line.
//
// A negative control builds this driver with `go build -overlay` over a
// sabotaged internal/codegen/moo file (tools/sabotage), so the modules it
// generates are the sabotaged emitter's. Nothing here uses real time, a socket
// or the network.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// scratch is the temporary directory this run generates into, if any; exit
// removes it on every path out.
var scratch string

func exit(code int) {
	if scratch != "" {
		_ = os.RemoveAll(scratch)
	}
	os.Exit(code)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "moo driver: "+format+"\n", args...)
	exit(2)
}

// generate generates one schema file, as its own unit, into dir.
func generate(path, dir string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	name := filepath.Base(path)
	tree, errs := parser.Parse(name, src)
	if len(errs) != 0 {
		fail("parse %s: %v", path, errs)
	}
	u, errs := check.Unit([]check.SourceFile{{Path: path, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: tree}})
	if len(errs) != 0 {
		fail("check %s: %v", path, errs)
	}
	files, err := moo.Generate(u)
	if err != nil {
		fail("generate %s: %v", path, err)
	}
	for n, data := range files {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil {
			fail("%v", err)
		}
	}
}

func main() {
	var schemas, inputs list
	flag.Var(&schemas, "schema", "a schema file to generate (repeatable; each is its own unit)")
	flag.Var(&inputs, "input", "name=path: a file the main reads as inputs[name] (repeatable)")
	mainPath := flag.String("main", "", "the gate's moo main")
	keep := flag.String("keep", "", "generate into this directory and keep it (default: a temporary one)")
	inputDir := flag.String("inputs", "", "a directory whose every file the main reads as inputs[basename]")
	readStdin := flag.Bool("stdin", false, "pass stdin to the main as inputs.stdin")
	analyze := flag.Bool("analyze", false, "first require moo-analyze to be silent over the modules and the main")
	flag.Parse()
	if *mainPath == "" || len(schemas) == 0 || flag.NArg() != 0 {
		fail("usage: driver -schema X.schema [-schema ...] [-input name=path ...] [-inputs dir] [-stdin] [-analyze] -main main.moo")
	}
	interpreter, why := moorun.Interpreter()
	if interpreter == "" {
		fail("%s", why)
	}
	dir := *keep
	if dir == "" {
		tmp, err := os.MkdirTemp("", "moo-packet-")
		if err != nil {
			fail("%v", err)
		}
		scratch, dir = tmp, tmp
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		fail("%v", err)
	}
	for _, s := range schemas {
		generate(s, dir)
	}
	program, err := os.ReadFile(*mainPath)
	if err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), program, 0o644); err != nil {
		fail("%v", err)
	}
	data := map[string][]byte{}
	for _, in := range inputs {
		name, path, ok := strings.Cut(in, "=")
		if !ok {
			fail("-input %q is not name=path", in)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			fail("%v", err)
		}
		data[name] = b
	}
	if *inputDir != "" {
		entries, err := os.ReadDir(*inputDir)
		if err != nil {
			fail("%v", err)
		}
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(*inputDir, e.Name()))
			if err != nil {
				fail("%v", err)
			}
			data[e.Name()] = b
		}
	}
	if *readStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fail("%v", err)
		}
		data["stdin"] = b
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.moo"), moorun.InputsModule(data), 0o644); err != nil {
		fail("%v", err)
	}
	if *analyze {
		analyzer, why := moorun.Analyzer()
		if analyzer == "" {
			fail("%s", why)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			fail("%v", err)
		}
		var mods []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".moo") && e.Name() != "inputs.moo" {
				mods = append(mods, e.Name())
			}
		}
		lint := exec.Command(analyzer, mods...)
		lint.Dir = dir
		// moo-analyze exits 0 on a lint warning: the gate is that it says nothing
		if out, _ := lint.CombinedOutput(); len(out) != 0 {
			fmt.Fprintf(os.Stderr, "FAILED: moo-analyze is not silent:\n%s", out)
			exit(1)
		}
	}
	cmd := exec.Command(interpreter, "main.moo")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, os.Stderr
	runErr := cmd.Run()
	if _, err := os.Stdout.Write(stdout.Bytes()); err != nil {
		fail("%v", err)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "moo driver: moo main.moo: %v\n", runErr)
		exit(1)
	}
	exit(0)
}
