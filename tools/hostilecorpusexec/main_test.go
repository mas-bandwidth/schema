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
			ok, err := run("test", binary, corpus, findings, "", 1, budget)
			if err != nil {
				t.Fatal(err)
			}
			if want := mode == "mismatch"; ok != want {
				t.Fatalf("known finding with %s: success=%v, want %v", mode, ok, want)
			}
		})
	}
}
