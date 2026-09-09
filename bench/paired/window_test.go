package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testWindow() windowEvidence {
	start := time.Date(2026, 9, 8, 22, 0, 0, 0, time.UTC)
	w := windowEvidence{Receipt: "synthetic fixture READY/START", CoordinatorPID: 100, Platform: "linux", Started: start.Format(time.RFC3339Nano), Ended: start.Add(2 * time.Second).Format(time.RFC3339Nano), Verdict: "OK", IntervalMillis: int(windowInterval / time.Millisecond)}
	for i, phase := range []string{"preflight", "start", "end"} {
		w.Samples = append(w.Samples, windowSample{Date: start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano), Phase: phase, Load: "1.00 1.00 1.00", Processes: []processSample{{PID: 1, PPID: 0, Name: "init", State: "S", Owned: true}, {PID: 100, PPID: 1, Name: "paired", State: "R", CPU: 1, Owned: true}, {PID: 101, PPID: 100, Name: "table-cpp", State: "R", CPU: 100, Owned: true}, {PID: 200, PPID: 1, Name: "WindowServer", State: "R", CPU: 30}}})
	}
	return w
}

func testPassWindow(rounds int) windowEvidence {
	w := testWindow()
	w.Samples = w.Samples[:2]
	appendPhase := func(phase string) {
		sample := testWindow().Samples[0]
		sample.Phase = phase
		sample.Date = time.Date(2026, 9, 8, 22, 0, 1, len(w.Samples)*int(time.Millisecond), time.UTC).Format(time.RFC3339Nano)
		w.Samples = append(w.Samples, sample)
	}
	appendRunner := func(label string) {
		appendPhase("before/" + label)
		appendPhase("after/" + label)
	}
	for _, wire := range []string{"packet", "table"} {
		appendRunner("control-start-" + wire)
	}
	for round := range rounds {
		for _, lang := range languages {
			for _, wire := range []string{"packet", "table"} {
				appendRunner(fmt.Sprintf("round-%d-%s-%s", round, lang, wire))
			}
		}
	}
	for _, wire := range []string{"packet", "table"} {
		appendRunner("control-end-" + wire)
	}
	appendPhase("end")
	w.Ended = w.Samples[len(w.Samples)-1].Date
	return w
}

func writeTestWindow(t *testing.T, dir string, rounds ...int) {
	t.Helper()
	n := 7
	if len(rounds) > 0 {
		n = rounds[0]
	}
	data, err := json.Marshal(testPassWindow(n))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "window.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPassWindowRequiresEveryRunnerBoundary(t *testing.T) {
	if err := validatePassWindow(testPassWindow(7), 7); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*windowEvidence){
		"only start and end": func(w *windowEvidence) { *w = testWindow() },
		"missing before":     func(w *windowEvidence) { w.Samples = append(w.Samples[:2], w.Samples[3:]...) },
		"missing after":      func(w *windowEvidence) { w.Samples = append(w.Samples[:3], w.Samples[4:]...) },
		"outside during":     func(w *windowEvidence) { w.Samples[2].Phase = "during/control-start-packet" },
		"wrong end":          func(w *windowEvidence) { w.Samples[3].Phase = "after/control-start-table" },
		"repeated runner": func(w *windowEvidence) {
			w.Samples[4].Phase = w.Samples[2].Phase
			w.Samples[5].Phase = w.Samples[3].Phase
		},
		"unknown runner":   func(w *windowEvidence) { w.Samples[2].Phase = "before/unknown" },
		"unmeasured round": func(w *windowEvidence) { *w = testPassWindow(8) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := testPassWindow(7)
			mutate(&w)
			if err := validatePassWindow(w, 7); err == nil {
				t.Fatal("incomplete runner monitoring was accepted")
			}
		})
	}
}

func TestProcessSnapshotNamesAndValidation(t *testing.T) {
	rows, err := parseProcesses([]byte("12 1 99.0 R /a private path/clang++\n13 12 0.0 S /usr/bin/table-cpp\n"))
	if err != nil || len(rows) != 2 || rows[0].Name != "clang++" || rows[1].Name != "table-cpp" {
		t.Fatalf("names-only process parsing: %+v, %v", rows, err)
	}
	for _, invalid := range []string{"", "1 2", "-1 2 0 S go", "1 2 NaN S go", "1 2 +Inf S go", "1 2 -1 S go", "1 bad 0 S go"} {
		if _, err := parseProcesses([]byte(invalid)); err == nil {
			t.Fatalf("accepted malformed process sample %q", invalid)
		}
	}
}

// This read-only opt-in checks the actual host monitor without opening a quiet
// window or starting clocks; other workers may remain active during this check.
func TestLocalProcessMonitor(t *testing.T) {
	if os.Getenv("SCHEMA_PAIRED_TEST_MONITOR") != "1" {
		t.Skip("set SCHEMA_PAIRED_TEST_MONITOR=1 to check the local ps/load monitor")
	}
	sample, err := windowSnapshot("monitor-check")
	if err != nil {
		t.Fatal(err)
	}
	if sample.Load == "" || sample.Load == "unavailable" {
		t.Fatal("local load unavailable")
	}
	found := false
	for _, process := range sample.Processes {
		if process.PID == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Fatal("local process monitor omitted the coordinator")
	}
}

func TestOwnedProcessesDoNotExemptSiblingWork(t *testing.T) {
	processes := []processSample{{PID: 1}, {PID: 10, PPID: 1}, {PID: 20, PPID: 10}, {PID: 21, PPID: 20}, {PID: 22, PPID: 21}, {PID: 30, PPID: 10}, {PID: 31, PPID: 30}, {PID: 40, PPID: 41}, {PID: 41, PPID: 40}}
	owned := ownedProcesses(processes, 20)
	for _, pid := range []int{1, 10, 20, 21, 22} {
		if !owned[pid] {
			t.Fatalf("actual ancestor/descendant %d not owned", pid)
		}
	}
	for _, pid := range []int{30, 31, 40, 41} {
		if owned[pid] {
			t.Fatalf("unrelated process %d was exempted", pid)
		}
	}
}

func TestWindowRefusesIncompleteOrCompetingEvidence(t *testing.T) {
	if err := validateWindow(testWindow()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*windowEvidence){
		"missing receipt":        func(w *windowEvidence) { w.Receipt = " " },
		"invalid verdict":        func(w *windowEvidence) { w.Verdict = "INVALID" },
		"incomplete end":         func(w *windowEvidence) { w.Ended = "" },
		"missing start":          func(w *windowEvidence) { w.Samples[1].Phase = "during" },
		"missing final sample":   func(w *windowEvidence) { w.Samples = w.Samples[:2] },
		"out of order":           func(w *windowEvidence) { w.Samples[1].Date = w.Ended; w.Samples[2].Date = w.Started },
		"gap":                    func(w *windowEvidence) { w.Ended = "2026-09-08T22:01:00Z"; w.Samples[2].Date = w.Ended },
		"unsupported monitor":    func(w *windowEvidence) { w.Platform = "windows" },
		"missing load":           func(w *windowEvidence) { w.Samples[1].Load = "unavailable" },
		"missing coordinator":    func(w *windowEvidence) { w.Samples[1].Processes = w.Samples[1].Processes[:1] },
		"false ownership":        func(w *windowEvidence) { w.Samples[1].Processes[3].Owned = true },
		"foreign idle compiler":  func(w *windowEvidence) { w.Samples[1].Processes[3].Name = "clang++"; w.Samples[1].Processes[3].CPU = 0 },
		"foreign busy benchmark": func(w *windowEvidence) { w.Samples[1].Processes[3].Name = "packet-go" },
		"foreign dotnet":         func(w *windowEvidence) { w.Samples[1].Processes[3].Name = "dotnet" },
		"unexpected path":        func(w *windowEvidence) { w.Samples[1].Processes[3].Name = "/usr/bin/clang" },
		"duplicate pid": func(w *windowEvidence) {
			w.Samples[1].Processes = append(w.Samples[1].Processes, w.Samples[1].Processes[0])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := testWindow()
			mutate(&w)
			if err := validateWindow(w); err == nil {
				t.Fatal("accepted invalid quiet-window evidence")
			}
		})
	}
}

func TestWindowRecordsDesktopLoadWithoutUniversalThreshold(t *testing.T) {
	w := testWindow()
	w.Samples[1].Load = "64.00 64.00 64.00"
	w.Samples[1].Processes[3].CPU = 200
	if err := validateWindow(w); err != nil {
		t.Fatal("invented a desktop CPU/load cutoff:", err)
	}
	w.Samples[1].Processes[3].Name = "clang++"
	w.Samples[1].Processes[3].State = "Z"
	if err := validateWindow(w); err != nil {
		t.Fatal("exited zombie process counted as competing:", err)
	}
}

func TestMissingReceiptKeepsInvalidEvidence(t *testing.T) {
	dir := t.TempDir()
	_, err := beginWindow(dir, "")
	if err == nil || !strings.Contains(err.Error(), "READY/START") {
		t.Fatal("missing operator receipt did not refuse:", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "window.json"))
	if err != nil {
		t.Fatal(err)
	}
	var w windowEvidence
	if err = json.Unmarshal(data, &w); err != nil || w.Verdict != "INVALID" || w.Reason == "" {
		t.Fatalf("failure lost its evidence: %s, %v", data, err)
	}
}

func TestUnfinishedWindowCannotPublish(t *testing.T) {
	dir := t.TempDir()
	w := &quietWindow{dir: dir, evidence: testWindow()}
	w.evidence.Verdict, w.evidence.Reason, w.evidence.Ended = "INVALID", "incomplete", ""
	if err := w.initializeJournal(); err != nil {
		t.Fatal(err)
	}
	if err := validateWindow(w.evidence); err == nil {
		t.Fatal("unfinished window was valid")
	}
}

func syntheticJournalWindow(dir string) *quietWindow {
	w := &quietWindow{dir: dir, evidence: testWindow()}
	w.evidence.Started = time.Now().UTC().Format(time.RFC3339Nano)
	w.evidence.Samples, w.evidence.Ended, w.evidence.Verdict, w.evidence.Reason = nil, "", "INVALID", "incomplete"
	w.snapshot = func(phase string) (windowSample, error) {
		sample := testWindow().Samples[0]
		sample.Phase, sample.Date = phase, time.Now().UTC().Format(time.RFC3339Nano)
		return sample, nil
	}
	return w
}

func readWindowEvidence(t *testing.T, dir string) windowEvidence {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "window.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence windowEvidence
	if err = json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func TestJournalAppendsEachSampleWithoutRewritingHistory(t *testing.T) {
	w := syntheticJournalWindow(t.TempDir())
	if err := w.initializeJournal(); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(w.dir, "window.json"))
	if err != nil {
		t.Fatal(err)
	}
	var previous []byte
	for i, phase := range []string{"preflight", "start", "before/synthetic", "during/synthetic", "after/synthetic"} {
		if err = w.sample(phase); err != nil {
			t.Fatal(err)
		}
		journal, err := os.ReadFile(filepath.Join(w.dir, windowJournal))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(w.evidence.Samples[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(journal, previous) || !bytes.Equal(journal[len(previous):], append(encoded, '\n')) || bytes.Count(journal, []byte{'\n'}) != i+1 {
			t.Fatal("journal did not append exactly one complete compact sample")
		}
		current, err := os.ReadFile(filepath.Join(w.dir, "window.json"))
		if err != nil || !bytes.Equal(marker, current) {
			t.Fatal("collection rewrote the window marker or growing history:", err)
		}
		previous = journal
	}
	if err = w.finish(errors.New("synthetic stopped pass")); err == nil {
		t.Fatal("stopped pass became successful")
	}
	final := readWindowEvidence(t, w.dir)
	if final.Verdict != "INVALID" || !reflect.DeepEqual(final.Samples, w.evidence.Samples) {
		t.Fatal("final refusal lost sample evidence")
	}
	if _, err = os.Stat(filepath.Join(w.dir, windowJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal remains after complete final evidence was persisted:", err)
	}
}

func TestCompletedJournalPreservesFinalManifestFormat(t *testing.T) {
	f := newProvenanceFixture(t, 7, "journal completion")
	w := syntheticJournalWindow(f.dir)
	phases := testPassWindow(7).Samples
	for _, sample := range phases[:len(phases)-1] {
		if err := w.sample(sample.Phase); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.finish(nil); err != nil {
		t.Fatal(err)
	}
	final := readWindowEvidence(t, f.dir)
	if !reflect.DeepEqual(final, w.evidence) || final.Verdict != "OK" {
		t.Fatal("final window differs from the complete collected evidence")
	}
	if _, err := os.Stat(filepath.Join(f.dir, windowJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed journal not removed:", err)
	}
	if err := sealPass(f.dir); err != nil {
		t.Fatal("completed window no longer fits the existing manifest:", err)
	}
	if err := verifyPass(f.dir); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedJournalCannotSeal(t *testing.T) {
	f := newProvenanceFixture(t, 7, "interrupted journal")
	w := syntheticJournalWindow(f.dir)
	if err := w.sample("preflight"); err != nil {
		t.Fatal(err)
	}
	marker := readWindowEvidence(t, f.dir)
	if marker.Verdict != "INVALID" || marker.Ended != "" || len(marker.Samples) != 0 {
		t.Fatal("in-progress marker claims complete evidence")
	}
	if err := sealPass(f.dir); err == nil {
		t.Fatal("interrupted journal was sealable")
	}
	// Even a complete-looking final window must not hide a leftover journal.
	writeTestWindow(t, f.dir, 7)
	if err := sealPass(f.dir); err == nil {
		t.Fatal("leftover journal was silently excluded from the raw manifest")
	}
}

func TestFinalWriteFailureRetainsJournalAndRefusesSeal(t *testing.T) {
	f := newProvenanceFixture(t, 7, "journal write failure")
	w := syntheticJournalWindow(f.dir)
	for _, phase := range []string{"preflight", "start"} {
		if err := w.sample(phase); err != nil {
			t.Fatal(err)
		}
	}
	journalPath := filepath.Join(f.dir, windowJournal)
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(f.dir, "window.json.tmp")
	if err = os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("synthetic interrupted pass")
	if err = w.finish(cause); err == nil || !errors.Is(err, cause) {
		t.Fatal("finalization failure lost the original refusal:", err)
	}
	after, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed final write removed or changed the journal:", err)
	}
	if err = sealPass(f.dir); err == nil {
		t.Fatal("failed finalization was sealable")
	}
	if err = os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err = w.finish(cause); !errors.Is(err, cause) {
		t.Fatal("retry changed the failed pass into success:", err)
	}
	final := readWindowEvidence(t, f.dir)
	if final.Verdict != "INVALID" || !reflect.DeepEqual(final.Samples, w.evidence.Samples) {
		t.Fatal("final retry lost sample evidence")
	}
	if _, err = os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal not removed after successful final persistence:", err)
	}
	if err = sealPass(f.dir); err == nil {
		t.Fatal("failed pass became sealable after persistence retry")
	}
}

// This subprocess waits without doing benchmark work. The parent test must
// terminate its own child when the synthetic monitor sees a foreign compiler.
func TestQuietWindowChild(t *testing.T) {
	if os.Getenv("SCHEMA_WINDOW_TEST_CHILD") != "1" {
		return
	}
	fmt.Print("child ready\n")
	time.Sleep(30 * time.Second)
}

func TestQuietWindowStopsOwnedRunnerAndRetainsRefusal(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCHEMA_WINDOW_TEST_CHILD", "1")
	w := syntheticJournalWindow(t.TempDir())
	w.snapshot = func(phase string) (windowSample, error) {
		sample := testWindow().Samples[0]
		sample.Phase = phase
		if strings.HasPrefix(phase, "during/") {
			sample.Processes[3].Name = "clang++"
		}
		return sample, nil
	}
	start := time.Now()
	out, err := w.capture(command{args: []string{executable, "-test.run=^TestQuietWindowChild$"}}, "synthetic-child")
	if err == nil || !strings.Contains(err.Error(), "competing process clang++") {
		t.Fatalf("foreign compiler did not stop owned runner: %q, %v", out, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("owned runner continued after the refusal")
	}
	if !strings.Contains(string(out), "child ready") {
		t.Fatal("owned subprocess never started")
	}
	if err = w.finish(err); err == nil {
		t.Fatal("refusal was lost during finalization")
	}
	data, err := os.ReadFile(filepath.Join(w.dir, "window.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence windowEvidence
	if err = json.Unmarshal(data, &evidence); err != nil || evidence.Verdict != "INVALID" || !strings.Contains(evidence.Reason, "clang++") {
		t.Fatalf("refusal evidence missing: %s, %v", data, err)
	}
}
