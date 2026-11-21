package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		case "mismatch":
			fmt.Println("refuse different_reason")
		case "match":
			fmt.Println("refuse damaged")
		case "exit":
			os.Exit(3)
		case "sanitizer":
			fmt.Fprintln(os.Stderr, "ERROR: AddressSanitizer: test marker")
			fmt.Println("refuse different_reason")
		case "hang":
			time.Sleep(time.Minute)
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

func TestKnownFindingCannotHideExecutionFailure(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	corpus := testCorpus(t, "probe", "answers")
	findings := filepath.Join(t.TempDir(), "findings.tsv") // beside the corpus, never in it: every .tsv there is a manifest
	if err := os.WriteFile(findings, []byte("probe\ttest-probe\tinputs/probe.bin\trefuse\tknown verdict mismatch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"mismatch", "match", "exit", "sanitizer", "hang", "error", "unbound", "unreadable", "skip"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", mode)
			budget := 5 * time.Second
			if mode == "hang" {
				budget = 250 * time.Millisecond
			}
			ok, err := run("test", binary, corpus, findings, "", "", 1, budget)
			if err != nil {
				t.Fatal(err)
			}
			if want := mode == "mismatch"; ok != want {
				t.Fatalf("known finding with %s: success=%v, want %v", mode, ok, want)
			}
		})
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
	write := func(answer string) string {
		p := filepath.Join(t.TempDir(), "known-findings.tsv")
		if err := os.WriteFile(p, []byte("probe\ttest-probe\tinputs/probe.bin\t"+answer+"\tknown disagreement\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, c := range []struct {
		name, mode, answer string
		want               bool
	}{
		{"CONTROL: an accept under the accept it records", "accept", "accept -", true},
		{"CONTROL: a refusal under refuse, whatever its reason", "mismatch", "refuse", true},
		{"an accept under refuse", "accept", "refuse", false},
		{"an accept under another digest", "accept", "accept " + strings.Repeat("b", 64), false},
		{"a refusal under an accept", "mismatch", "accept -", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER", c.mode)
			ok, err := run("test", binary, corpus, write(c.answer), "", "", 1, 5*time.Second)
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
