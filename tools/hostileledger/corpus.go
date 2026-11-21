// THE LEDGER CORPUS: committed runner logs the ledger's own judgement is
// replayed over (testdata/ledger-corpus).
//
// A finding against the ledger is a runner log and the verdict it owes. Each
// case is a log (<id>/log.txt, with the held lists it is checked under beside
// it) over the small registry in corpus/, and CASES.tsv gives its verdict:
// `pass`, or `red:` and words the refusal must say. The unit test
// (TestLedgerCorpus) and `hostileledger -ledger-corpus` read the same files;
// `make hostile-ledger-corpus` runs the second, and the generated-Go
// hostile-corpus gate (test-go, the nightly) depends on it. An empty index, a
// case with no verdict and a case folder the index does not name are errors.
//
//	CASES.tsv   id <TAB> verdict <TAB> flags <TAB> what the case is
//	            flags, space separated or `-`: runs=N  rows=<carrier>=<row>,<row>
//	            skips (read <id>/skips.tsv)  carriers (read <id>/carriers.tsv)
//	            crlf (check the log with CRLF line endings)
//	            registry (hold the log to <id>/registry/ in place of corpus/)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ledgerCase struct {
	id, verdict, flags, what string
}

func readLedgerCorpus(dir string) ([]ledgerCase, error) {
	data, err := os.ReadFile(filepath.Join(dir, "CASES.tsv"))
	if err != nil {
		return nil, err
	}
	var cases []ledgerCase
	seen := map[string]bool{"CASES.tsv": true, "corpus": true}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[0] == "" || f[3] == "" || (f[1] != "pass" && (!strings.HasPrefix(f[1], "red:") || len(f[1]) == len("red:"))) {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: a case is id, verdict (pass, or red:<what the refusal says>), flags and what it is", dir, n+1)
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: the case %s is named twice", dir, n+1, f[0])
		}
		seen[f[0]] = true
		cases = append(cases, ledgerCase{f[0], f[1], f[2], f[3]})
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s/CASES.tsv names no case: a corpus run over nothing passes nothing", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !seen[e.Name()] {
			return nil, fmt.Errorf("%s/%s is in the corpus folder and CASES.tsv does not name it: an unread case", dir, e.Name())
		}
	}
	return cases, nil
}

// runLedgerCase checks one case's log and holds the outcome to its verdict.
func runLedgerCase(dir string, c ledgerCase, scratch string) error {
	runs, rows, skips, carriers, crlf := 1, "", "", "", false
	registry := filepath.Join(dir, "corpus")
	if c.flags != "-" {
		for flag := range strings.FieldsSeq(c.flags) {
			key, value, _ := strings.Cut(flag, "=")
			switch key {
			case "runs":
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("flag %q: %w", flag, err)
				}
				runs = n
			case "rows":
				rows = value
			case "skips":
				skips = filepath.Join(dir, c.id, "skips.tsv")
			case "carriers":
				carriers = filepath.Join(dir, c.id, "carriers.tsv")
			case "registry":
				registry = filepath.Join(dir, c.id, "registry")
			case "crlf":
				crlf = true
			default:
				return fmt.Errorf("flag %q is not one the corpus knows", flag)
			}
		}
	}
	logPath := filepath.Join(dir, c.id, "log.txt")
	if crlf {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return err
		}
		logPath = filepath.Join(scratch, c.id+".crlf.txt")
		if err := os.WriteFile(logPath, []byte(strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n", "\r\n")), 0o644); err != nil {
			return err
		}
	} else if _, err := os.Stat(logPath); err != nil {
		return err
	}
	out, err := check("corpus", registry, logPath, runs, skips, carriers, rows)
	if c.verdict == "pass" {
		if err != nil {
			return fmt.Errorf("the case owes a pass and the ledger is red: %w", err)
		}
		if !strings.Contains(out, "negative controls: ") {
			return fmt.Errorf("the case passed without the tool's negative controls: %s", out)
		}
		return nil
	}
	want := strings.TrimPrefix(c.verdict, "red:")
	if err == nil {
		return fmt.Errorf("the case owes a refusal saying %q and passed", want)
	}
	if !strings.Contains(err.Error(), want) {
		return fmt.Errorf("the case owes a refusal saying %q and got: %w", want, err)
	}
	return nil
}

func runLedgerCorpus(dir, scratch string) ([]string, error) {
	cases, err := readLedgerCorpus(dir)
	if err != nil {
		return nil, err
	}
	var lines, bad []string
	for _, c := range cases {
		if err := runLedgerCase(dir, c, scratch); err != nil {
			bad = append(bad, c.id+": "+err.Error())
			lines = append(lines, fmt.Sprintf("ledger-corpus FAIL %s (%s): %v", c.id, c.what, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("ledger-corpus ok   %s: %s (%s)", c.id, c.verdict, c.what))
	}
	if len(bad) > 0 {
		return lines, fmt.Errorf("%d of %d ledger-corpus case(s) did not come to their verdict:\n  %s", len(bad), len(cases), strings.Join(bad, "\n  "))
	}
	lines = append(lines, fmt.Sprintf("ledger-corpus: all %d case(s) of %s came to their verdict", len(cases), dir))
	return lines, nil
}
