package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The child is a deterministic case binary; it does not execute a reader.
// Exercise the real subprocess/parser/aggregate path, including an existing
// finding, so an infrastructure failure cannot hide behind a semantic waiver.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SCHEMA_HOSTILE_RUNNER_TEST_ANSWER"); mode != "" {
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
	corpus := t.TempDir()
	manifest := "probe\twire\tprobe.schema\tRoot\tinputs/probe.bin\ttest\trefuse\tdamaged\t-\n"
	if err := os.WriteFile(filepath.Join(corpus, "manifest.tsv"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	findings := filepath.Join(corpus, "findings.tsv")
	if err := os.WriteFile(findings, []byte("probe\ttest-probe\tinputs/probe.bin\tknown verdict mismatch\n"), 0o600); err != nil {
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
