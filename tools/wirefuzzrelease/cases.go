// THE GATE CASES: committed fixtures the release wire-fuzz gate's judgement is
// replayed over (testdata/gate-cases).
//
// A `row` case is a harness log, <id>.log, with the run's coordinates, and
// the verdict its row owes. A `table` case is a leg's roster with named rows
// taken away, and the verdict the leg owes. The unit test (TestGateCases) and
// `wirefuzzrelease cases` read the same files; every run of the gate
// (make/wire-fuzz-release.mk) replays them first. An empty index, a case with
// no verdict and a file the index does not name are errors.
//
//	CASES.tsv  id <TAB> verdict <TAB> kind <TAB> spec <TAB> what the case is
//	           verdict: pass, or red:<words the RED verdict must say>
//	           row spec:   <leg> <arm> <seed> <exit status> <n asked>
//	           table spec: <leg> <row removed, or ->
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GateCasesDir is the committed case folder, from the repository root.
const GateCasesDir = "tools/wirefuzzrelease/testdata/gate-cases"

type gateCase struct {
	id, verdict, kind, spec, what string
}

func readGateCases(dir string) ([]gateCase, error) {
	data, err := os.ReadFile(filepath.Join(dir, "CASES.tsv"))
	if err != nil {
		return nil, err
	}
	var out []gateCase
	named := map[string]bool{"CASES.tsv": true}
	seen := map[string]bool{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 || f[0] == "" || (f[1] != "pass" && (!strings.HasPrefix(f[1], "red:") || len(f[1]) == len("red:"))) || (f[2] != "row" && f[2] != "table") {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: a case is id, verdict (pass, or red:<what the verdict says>), kind (row or table), spec and what it is", dir, n+1)
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: the case %s is named twice", dir, n+1, f[0])
		}
		seen[f[0]] = true
		if f[2] == "row" {
			named[f[0]+".log"] = true
		}
		out = append(out, gateCase{f[0], f[1], f[2], f[3], f[4]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s/CASES.tsv names no case: a run over nothing passes nothing", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !named[e.Name()] {
			return nil, fmt.Errorf("%s/%s is in the case folder and CASES.tsv does not name it: an unread case", dir, e.Name())
		}
	}
	return out, nil
}

// runGateCase replays one case and returns the verdict line it came to.
func runGateCase(dir string, c gateCase, scratch string) (string, error) {
	work := filepath.Join(scratch, c.id)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	none := filepath.Join(work, "none.txt")
	if err := os.WriteFile(none, []byte("# none\n"), 0o644); err != nil {
		return "", err
	}
	f := strings.Fields(c.spec)
	switch c.kind {
	case "row":
		if len(f) != 5 {
			return "", fmt.Errorf("a row spec is leg, arm, seed, exit status and n asked: %q", c.spec)
		}
		status, err1 := strconv.Atoi(f[3])
		asked, err2 := strconv.Atoi(f[4])
		if err1 != nil || err2 != nil {
			return "", fmt.Errorf("a row spec's exit status and n are numbers: %q", c.spec)
		}
		log, err := os.ReadFile(filepath.Join(dir, c.id+".log"))
		if err != nil {
			return "", err
		}
		base := f[0] + "." + f[1] + "." + f[2]
		if err := os.WriteFile(filepath.Join(work, base+".log"), log, 0o644); err != nil {
			return "", err
		}
		if err := writeRow(f[0], f[1], f[2], work, none, none, false, status, 3, asked); err != nil {
			return "", err
		}
		row, err := os.ReadFile(filepath.Join(work, base+".row"))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(strings.SplitN(string(row), "\t", 4)[3]), nil
	default:
		if len(f) != 2 {
			return "", fmt.Errorf("a table spec is leg and the row removed (or -): %q", c.spec)
		}
		arms, seeds := []string{"plain", "retain"}, []string{"1", "2", "3"}
		for _, arm := range arms {
			for _, seed := range seeds {
				name := f[0] + "." + arm + "." + seed + ".row"
				if name == f[1] {
					continue
				}
				row := f[0] + "\t" + arm + "\t" + seed + "\tpass (100000 mutants, 3 s)\n"
				if err := os.WriteFile(filepath.Join(work, name), []byte(row), 0o644); err != nil {
					return "", err
				}
			}
		}
		out, ok, err := table(f[0], work, arms, seeds, "100000")
		if err != nil {
			return "", err
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		last := lines[len(lines)-1]
		if ok {
			return "pass (" + last + ")", nil
		}
		return "RED: " + out, nil
	}
}

func holdGateVerdict(c gateCase, got string, err error) error {
	if err != nil {
		return err
	}
	if c.verdict == "pass" {
		if !strings.HasPrefix(got, "pass (") {
			return fmt.Errorf("the case owes a pass and came to: %s", got)
		}
		return nil
	}
	want := strings.TrimPrefix(c.verdict, "red:")
	if !strings.HasPrefix(got, "RED") || !strings.Contains(got, want) {
		return fmt.Errorf("the case owes a RED saying %q and came to: %s", want, got)
	}
	return nil
}

func runGateCases(dir, scratch string) ([]string, error) {
	cases, err := readGateCases(dir)
	if err != nil {
		return nil, err
	}
	var lines, bad []string
	for _, c := range cases {
		got, err := runGateCase(dir, c, scratch)
		if err := holdGateVerdict(c, got, err); err != nil {
			bad = append(bad, c.id+": "+err.Error())
			lines = append(lines, fmt.Sprintf("wire-fuzz-release cases FAIL %s (%s): %v", c.id, c.what, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("wire-fuzz-release cases ok   %s: %s (%s)", c.id, c.verdict, c.what))
	}
	if len(bad) > 0 {
		return lines, fmt.Errorf("%d of %d wire-fuzz-release case(s) did not come to their verdict:\n  %s", len(bad), len(cases), strings.Join(bad, "\n  "))
	}
	lines = append(lines, fmt.Sprintf("wire-fuzz-release cases: all %d case(s) of %s came to their verdict", len(cases), dir))
	return lines, nil
}
