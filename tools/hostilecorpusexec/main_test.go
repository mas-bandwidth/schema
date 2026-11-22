package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// The child is a deterministic case binary; it does not execute a reader.
// Exercise the real subprocess/parser/aggregate path, including an existing
// finding, so an infrastructure failure cannot hide behind a semantic waiver.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER"); mode != "" {
		switch mode {
		case "scorer-corpus":
			if len(os.Args) > 1 && os.Args[1] == "run" {
				main()
			} else if reply := os.Getenv("SCHEMA_HOSTILE_RUNNER_TEST_REPLY"); reply == "exit" {
				os.Exit(3)
			} else {
				fmt.Print(reply)
			}
		case "mismatch":
			fmt.Println("accept -")
		case "match":
			fmt.Println("refuse damaged")
		case "exit":
			os.Exit(3)
		case "sanitizer":
			fmt.Fprintln(os.Stderr, "ERROR: AddressSanitizer: test marker")
			fmt.Println("refuse different_reason")
		case "hang":
			// block for good on a pipe this process holds both ends of:
			// only a kill ends it, and no clock is read
			r, w, err := os.Pipe()
			if err != nil {
				os.Exit(5)
			}
			_, _ = r.Read(make([]byte, 1))
			_ = w.Close()
		case "error":
			fmt.Println("error test failure")
		case "unbound":
			fmt.Println("unbound missing binding")
		case "unreadable":
			fmt.Println("not a case answer")
		case "skip":
			fmt.Println("skip no reader")
		default:
			os.Exit(4)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

var realBudget = startBudget

func TestKnownFindingCannotHideExecutionFailure(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// the committed scenario corpus: one manifest case, the finding that
	// carries it, and one row per scenario
	corpus := filepath.Join("testdata", "scenarios")
	data, err := os.ReadFile(filepath.Join(corpus, "scenarios.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("scenarios.tsv line %d: %d fields, want 5", n+1, len(f))
		}
		name, mode, budget, carried, want := f[0], f[1], f[2], f[3], f[4]
		findings := ""
		switch carried {
		case "known":
			findings = filepath.Join(corpus, "findings.tsv")
		case "none":
		default:
			t.Fatalf("scenarios.tsv line %d: findings %q", n+1, carried)
		}
		if budget != "spent" && budget != "never" {
			t.Fatalf("scenarios.tsv line %d: budget %q", n+1, budget)
		}
		if want != "true" && want != "false" {
			t.Fatalf("scenarios.tsv line %d: success %q", n+1, want)
		}
		seen[budget+"/"+want]++
		t.Run(name, func(t *testing.T) {
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", mode)
			// the budget is injected, never read off a clock: a spent one
			// has already run out, a never one never does
			startBudget = func(time.Duration) (<-chan time.Time, func() bool) {
				if budget == "spent" {
					spent := make(chan time.Time, 1)
					spent <- time.Time{}
					return spent, func() bool { return true }
				}
				return nil, func() bool { return true }
			}
			t.Cleanup(func() { startBudget = realBudget })
			ok, err := run("test", binary, corpus, findings, "", "", 1, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if ok != (want == "true") {
				t.Fatalf("%s (child %s, budget %s, findings %s): success=%v, want %s", name, mode, budget, carried, ok, want)
			}
		})
	}
	// an over-budget refusal is only proven beside a completed run that passes
	if seen["spent/false"] == 0 || seen["never/true"] == 0 {
		t.Errorf("the scenario corpus needs an over-budget failure and a completed success; it holds %v", seen)
	}
}

// TestSkipsAreHeldByName is CI-TOOLS-3: a skip passes only when known-skips.tsv
// names its case, so a binding dropped from a case binary reads red rather
// than as one more skip; and a carried skip whose case answers, or is gone
// from the manifest, is stale.
func TestSkipsAreHeldByName(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	manifest := "probe\twire\tprobe.schema\tRoot\tinputs/probe.bin\ttest\trefuse\tdamaged\t-\n"
	if err := os.WriteFile(filepath.Join(corpus, "manifest.tsv"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	carried := write("known-skips.tsv", "# name\twhy\nprobe\tno file-form load for this root\n")
	gone := write("known-skips.tsv", "probe\tno file-form load for this root\nno-such-case\tgone from the manifest\n")
	for _, c := range []struct {
		name, mode, skips string
		want              bool
	}{
		{"a skip no line names", "skip", "", false},
		{"a skip the list names", "skip", carried, true},
		{"a carried skip whose case answers", "match", carried, false},
		{"a carried skip whose case is gone", "skip", gone, false},
		{"no skip and no list", "match", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", c.mode)
			ok, err := run("test", binary, corpus, "", c.skips, "", 1, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if ok != c.want {
				t.Fatalf("success=%v, want %v", ok, c.want)
			}
		})
	}
	if _, _, err := loadSkips(write("bad.tsv", "probe\n")); err == nil {
		t.Error("a skip line with no reason was accepted")
	}
}

// TestScorerReadsVerdictsAndAcceptedValues runs the CLI and its real case
// subprocess on outcomes only. The worker returns the fixture's response
// without invoking a codec. Refusal diagnoses may differ across ports.
func TestScorerReadsVerdictsAndAcceptedValues(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open("testdata/scorer-outcomes.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma, reader.Comment, reader.FieldsPerRecord = '\t', '#', 7
	rows, err := reader.ReadAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("read scorer outcome fixture: %v (%d rows)", err, len(rows))
	}
	for _, row := range rows {
		name, verdict, reason, digest := row[0], row[1], row[2], row[3]
		reply, body, expectedExit := row[4], row[5], row[6]
		t.Run(name, func(t *testing.T) {
			corpus := t.TempDir()
			manifest := strings.Join([]string{name, "text", "probe.schema", "Probe", "inputs/probe.json",
				"scorer outcome", verdict, reason, digest}, "\t") + "\n"
			if err := os.WriteFile(filepath.Join(corpus, "manifest.tsv"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			if body != "-" {
				reply += "\n" + body
			}
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", "scorer-corpus")
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_REPLY", reply)
			cmd := testproc.Command(t, binary, "run", "-port", "test", "-exec", binary, "-jobs", "1", "-corpus", corpus)
			output, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil {
				t.Fatalf("could not start scorer: %v", err)
			}
			if got := cmd.ProcessState.ExitCode(); fmt.Sprint(got) != expectedExit {
				t.Fatalf("scorer exit %d, want %s\n%s", got, expectedExit, output)
			}
		})
	}
}
