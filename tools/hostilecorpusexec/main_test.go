package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// The child is a deterministic case binary; it does not execute a reader.
// Exercise the real subprocess/parser/aggregate path, including an existing
// finding, so an infrastructure failure cannot hide behind a semantic waiver.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER"); mode != "" {
		// the corpus's second case always answers, so a run whose first case
		// skips is not a run of nothing (the ledger refuses that)
		if strings.HasSuffix(os.Args[len(os.Args)-1], "answers.bin") {
			fmt.Println("refuse damaged")
			os.Exit(0)
		}
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
		case "otherreason":
			// a refusal is graded by its verdict alone (F-222)
			fmt.Println("refuse different_reason")
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
		case "accept":
			fmt.Println("accept -")
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
	scenarios := filepath.Join("testdata", "scenarios")
	corpus := filepath.Join(scenarios, "corpus")
	data, err := os.ReadFile(filepath.Join(scenarios, "scenarios.tsv"))
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
			findings = filepath.Join(scenarios, "findings.tsv")
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

// TestAFindingCarriesTheAnswerItRecords is F-079: a finding names the answer
// the port gives today, and carries that answer and no other. Before, any
// accept or refusal was carried under any finding, so a port that changed from
// refusing to accepting (or to another digest) stayed green. The controls are
// the findings that record the very answer given; a finding with no answer is
// refused outright.
func TestAFindingCarriesTheAnswerItRecords(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	corpus := testCorpus(t, "probe", "answers")
	// a refusal is graded by its verdict alone (F-222), so a refusal is a
	// disagreement to carry only where the case expects an accept
	accepting := testCorpus(t, "probe", "answers")
	manifest := filepath.Join(accepting, "manifest.tsv")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "inputs/probe.bin\ttest\trefuse\tdamaged\t-", "inputs/probe.bin\ttest\taccept\t-\t"+strings.Repeat("a", 64), 1))
	if err := os.WriteFile(manifest, data, 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(answer string) string {
		p := filepath.Join(t.TempDir(), "known-findings.tsv")
		if err := os.WriteFile(p, []byte("probe\ttest-probe\tinputs/probe.bin\t"+answer+"\tknown disagreement\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, c := range []struct {
		name, mode, answer string
		accepts            bool
		want               bool
	}{
		{"CONTROL: an accept under the accept it records", "accept", "accept -", false, true},
		{"CONTROL: a refusal under refuse, whatever its reason", "otherreason", "refuse", true, true},
		{"an accept under refuse", "accept", "refuse", false, false},
		{"an accept under another digest", "accept", "accept " + strings.Repeat("b", 64), false, false},
		{"a refusal under an accept", "otherreason", "accept -", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", c.mode)
			dir := corpus
			if c.accepts {
				dir = accepting
			}
			ok, err := run("test", binary, dir, write(c.answer), "", "", 1, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if ok != c.want {
				t.Fatalf("success=%v, want %v", ok, c.want)
			}
		})
	}
	old := filepath.Join(t.TempDir(), "known-findings.tsv")
	if err := os.WriteFile(old, []byte("probe\ttest-probe\tinputs/probe.bin\tknown disagreement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadFindings(old); err == nil {
		t.Error("a finding that records no answer was read: it would carry any answer")
	}
	for _, carry := range []string{"answer-matches", "answer-refuse", "answer-other-digest"} {
		if _, _, err := loadFindings(filepath.Join("..", "..", "internal", "ci", "hostilecorpus", "testdata", "carry", "findings", carry+".tsv")); err != nil {
			t.Errorf("the committed carry control %s does not read: %v", carry, err)
		}
	}
	if _, _, err := loadFindings(filepath.Join("..", "..", "internal", "ci", "hostilecorpus", "testdata", "carry", "findings", "no-answer.tsv")); err == nil {
		t.Error("the committed no-answer carry control reads: it must be refused")
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
	corpus := testCorpus(t, "probe", "answers")
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

// testCorpus plants a corpus folder: one wire case per name, each with its
// input file, which is what the ledger reads the registered count from.
func testCorpus(t *testing.T, names ...string) string {
	t.Helper()
	corpus := t.TempDir()
	if err := os.MkdirAll(filepath.Join(corpus, "inputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n"
	for _, name := range names {
		manifest += name + "\twire\tprobe.schema\tRoot\tinputs/" + name + ".bin\ttest\trefuse\tdamaged\t-\n"
		if err := os.WriteFile(filepath.Join(corpus, "inputs", name+".bin"), []byte{0}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(corpus, "manifest.tsv"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return corpus
}

// TestARunOfNothingIsRed is the ledger on this runner: cases answered equals
// cases the folder registers. A corpus whose every case is skipped by name
// compared nothing, and a manifest with no row is not a corpus; before the
// ledger both printed a summary and exited 0. The control is the same run
// with one case that answers.
func TestARunOfNothingIsRed(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	skips := filepath.Join(t.TempDir(), "known-skips.tsv")
	if err := os.WriteFile(skips, []byte("probe\tno reader\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", "skip")

	ok, err := run("test", binary, testCorpus(t, "probe", "answers"), "", skips, "", 1, 5*time.Second)
	if err != nil || !ok {
		t.Fatalf("CONTROL FAILED: one case skipped by name beside one that answers: success=%v, %v", ok, err)
	}
	ok, err = run("test", binary, testCorpus(t, "probe"), "", skips, "", 1, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a run whose every case is skipped passed: it compared nothing")
	}
	if _, err := run("test", binary, testCorpus(t), "", "", "", 1, 5*time.Second); err == nil || !strings.Contains(err.Error(), "registers no case") {
		t.Errorf("a corpus with no case is not refused: %v", err)
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
			if err := os.MkdirAll(filepath.Join(corpus, "inputs"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(corpus, "inputs", "probe.json"), []byte("{}"), 0o600); err != nil {
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
