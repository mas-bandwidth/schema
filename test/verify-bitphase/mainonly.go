// THE MAIN CORPUS'S OWN STEPS. The compressed-float decode case
// (cfdecode.go) and the float32 literal case (f32lit.go) read types only the
// main BitPhase.schema declares (CfDecode, F32Const, F32Lit). Every driver
// source carries those steps between two marker lines,
//
//	@MAIN_ONLY@
//	...
//	@END_MAIN_ONLY@
//
// and every source passes through withCfDecodeSteps, which keeps the steps
// (dropping the two marker lines) on a main run and drops them whole on a
// case family's run (--family), whose schema declares none of those types. A
// family run then holds the driver's output to having no CFDECODE or F32LIT
// line at all.
package main

import (
	"fmt"
	"strings"
)

const (
	mainOnlyBegin = "@MAIN_ONLY@"
	mainOnlyEnd   = "@END_MAIN_ONLY@"
)

// familyRun says the run is a case family's, not the main corpus's: set by
// runFamily before any driver source is written.
var familyRun bool

// mainOnlySteps keeps or drops the marked steps of a driver source. The
// markers must stand alone on their lines, open before they close, and never
// nest: anything else panics, since the source is the harness's own.
func mainOnlySteps(src string, keep bool) string {
	var out []string
	in := false
	for n, line := range strings.Split(src, "\n") {
		switch strings.TrimSpace(line) {
		case mainOnlyBegin:
			if in {
				panic(fmt.Sprintf("driver source line %d: %s inside a main-only block", n+1, mainOnlyBegin))
			}
			in = true
			continue
		case mainOnlyEnd:
			if !in {
				panic(fmt.Sprintf("driver source line %d: %s with no block open", n+1, mainOnlyEnd))
			}
			in = false
			continue
		}
		if strings.Contains(line, mainOnlyBegin) || strings.Contains(line, mainOnlyEnd) {
			panic(fmt.Sprintf("driver source line %d: a main-only marker shares its line: %q", n+1, line))
		}
		if !in || keep {
			out = append(out, line)
		}
	}
	if in {
		panic("driver source: a main-only block is never closed")
	}
	return strings.Join(out, "\n")
}

// checkNoMainOnlyOutput holds a family run's output to having none of the
// main corpus's step lines: a step that ran anyway is a driver whose markers
// are wrong.
func checkNoMainOnlyOutput(lang, out string) error {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "CFDECODE") || strings.HasPrefix(line, "F32LIT") {
			return fmt.Errorf("FAILED: %s ran a main-corpus step in a family run: %q", lang, line)
		}
	}
	return nil
}
