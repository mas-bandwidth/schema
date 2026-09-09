package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const windowInterval = 2 * time.Second
const maximumSampleGap = 10 * time.Second
const maximumWindowSamples = 10000
const windowJournal = "window.samples.jsonl"

// A receipt identifies the operator's READY/START coordination record. It is an
// attestation, not machine proof that every participant actually became quiet.
// Process evidence supplements that record; sampling cannot detect every brief
// or unusually named workload, and load has no universal desktop cutoff.
type windowEvidence struct {
	Receipt        string         `json:"receipt"`
	CoordinatorPID int            `json:"coordinator_pid"`
	Platform       string         `json:"platform"`
	Started        string         `json:"started"`
	Ended          string         `json:"ended,omitempty"`
	Verdict        string         `json:"verdict"`
	Reason         string         `json:"reason,omitempty"`
	IntervalMillis int            `json:"interval_ms"`
	Samples        []windowSample `json:"samples"`
}

type windowSample struct {
	Date      string          `json:"date"`
	Phase     string          `json:"phase"`
	Load      string          `json:"load"`
	Processes []processSample `json:"processes"`
}

// Only executable names are recorded: never arguments, executable paths, or
// environment variables. CPU is ps's platform-defined percentage, not a claim
// that Linux lifetime averages and Darwin recent averages mean the same thing.
type processSample struct {
	PID   int     `json:"pid"`
	PPID  int     `json:"ppid"`
	Name  string  `json:"name"`
	State string  `json:"state"`
	CPU   float64 `json:"cpu_pct"`
	Owned bool    `json:"owned"`
}

func parseProcesses(data []byte) ([]processSample, error) {
	var processes []processSample
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 5 {
			return nil, errors.New("incomplete process sample")
		}
		pid, errPID := strconv.Atoi(fields[0])
		ppid, errPPID := strconv.Atoi(fields[1])
		cpu, errCPU := strconv.ParseFloat(fields[2], 64)
		if errPID != nil || errPPID != nil || errCPU != nil || pid <= 0 || ppid < 0 || cpu < 0 || math.IsNaN(cpu) || math.IsInf(cpu, 0) {
			return nil, errors.New("invalid process sample")
		}
		processes = append(processes, processSample{PID: pid, PPID: ppid, CPU: cpu, State: fields[3], Name: filepath.Base(strings.Join(fields[4:], " "))})
	}
	if len(processes) == 0 {
		return nil, errors.New("empty process sample")
	}
	return processes, nil
}

// Exclude the coordinator, its descendants, and its direct ancestor chain.
// Siblings under the same shell/app stay foreign; exempting every descendant of
// an ancestor would hide another agent's compiler or benchmark on this bench.
func ownedProcesses(processes []processSample, coordinator int) map[int]bool {
	parents := make(map[int]int, len(processes))
	for _, p := range processes {
		parents[p.PID] = p.PPID
	}
	owned := map[int]bool{coordinator: true}
	for parent := parents[coordinator]; parent > 0 && !owned[parent]; parent = parents[parent] {
		owned[parent] = true
	}
	for _, p := range processes {
		seen := map[int]bool{}
		for pid := p.PID; pid > 0 && !seen[pid]; pid = parents[pid] {
			if pid == coordinator {
				owned[p.PID] = true
				break
			}
			seen[pid] = true
		}
	}
	return owned
}

// These named build/benchmark processes refuse even at 0% sampled CPU: an idle
// compiler server or sleeping benchmark can resume between samples. General
// desktop apps are recorded but are not assigned an invented load threshold.
// Generic interpreter workloads cannot be classified from names alone.
func competingProcess(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case "schema", "go", "compile", "link", "asm", "make", "gmake", "cmake", "ninja", "cargo", "rustc", "dotnet", "msbuild", "vbcscompiler", "csc", "cc", "c++", "gcc", "g++", "clang", "clang++", "cc1", "cc1plus", "ld", "ld.lld", "lld", "javac", "dart", "elixirc", "mix", "swift", "swiftc", "xcodebuild", "paired":
		return true
	}
	for _, prefix := range []string{"schema_bench", "schema_test", "schemabench", "bench", "table_main", "packet-", "table-", "clang-", "clang++-", "gcc-", "g++-", "cc1", "rustc-", "paired.test"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func inspectSample(sample windowSample, coordinator int) error {
	owned := ownedProcesses(sample.Processes, coordinator)
	seen := map[int]bool{}
	for _, p := range sample.Processes {
		if p.PID <= 0 || p.PPID < 0 || p.Name == "" || p.Name != filepath.Base(p.Name) || p.State == "" || p.CPU < 0 || math.IsNaN(p.CPU) || math.IsInf(p.CPU, 0) || seen[p.PID] {
			return errors.New("invalid or duplicate recorded process")
		}
		seen[p.PID] = true
		if p.Owned != owned[p.PID] {
			return fmt.Errorf("incorrect ownership for process %d", p.PID)
		}
		if !p.Owned && !strings.HasPrefix(p.State, "Z") && competingProcess(p.Name) {
			return fmt.Errorf("competing process %s (PID %d, CPU %.1f%%) during %s", p.Name, p.PID, p.CPU, sample.Phase)
		}
	}
	if !seen[coordinator] {
		return errors.New("coordinator missing from process sample")
	}
	if sample.Load == "" || sample.Load == "unavailable" {
		return errors.New("load sample unavailable")
	}
	return nil
}

func validateWindow(w windowEvidence) error {
	if w.Verdict != "OK" || w.Reason != "" || strings.TrimSpace(w.Receipt) == "" || w.CoordinatorPID <= 0 || (w.Platform != "darwin" && w.Platform != "linux") || w.IntervalMillis != int(windowInterval/time.Millisecond) {
		return errors.New("quiet window is invalid or lacks its operator receipt")
	}
	start, err := time.Parse(time.RFC3339Nano, w.Started)
	if err != nil {
		return errors.New("quiet window lacks valid START")
	}
	end, err := time.Parse(time.RFC3339Nano, w.Ended)
	if err != nil || end.Before(start) {
		return errors.New("quiet window lacks valid END")
	}
	if len(w.Samples) < 3 || len(w.Samples) > maximumWindowSamples || w.Samples[0].Phase != "preflight" || w.Samples[1].Phase != "start" || w.Samples[len(w.Samples)-1].Phase != "end" {
		return errors.New("quiet window sampling is incomplete")
	}
	previous := start
	for _, sample := range w.Samples {
		at, err := time.Parse(time.RFC3339Nano, sample.Date)
		if err != nil || at.Before(previous) || at.After(end) || at.Sub(previous) > maximumSampleGap || sample.Phase == "" {
			return errors.New("quiet window has an invalid timestamp or sampling gap")
		}
		if err = inspectSample(sample, w.CoordinatorPID); err != nil {
			return err
		}
		previous = at
	}
	if end.Sub(previous) > maximumSampleGap {
		return errors.New("quiet window END lacks current process evidence")
	}
	return nil
}

// A START/END pair alone cannot certify all measured work. Bind runner phases
// to the exact control and round file names the completed pass must contain.
func validatePassWindow(w windowEvidence, rounds int) error {
	if err := validateWindow(w); err != nil {
		return err
	}
	if rounds < 7 || rounds > maximumWindowSamples/16 {
		return errors.New("quiet window has an invalid round count")
	}
	want := map[string]int{}
	for _, wire := range []string{"packet", "table"} {
		want["control-start-"+wire] = 0
		want["control-end-"+wire] = rounds + 1
		for round := range rounds {
			for _, lang := range languages {
				want[fmt.Sprintf("round-%d-%s-%s", round, lang, wire)] = round + 1
			}
		}
	}
	seen := map[string]bool{}
	active := ""
	group := 0
	for _, sample := range w.Samples[2 : len(w.Samples)-1] {
		kind, label, ok := strings.Cut(sample.Phase, "/")
		phaseGroup, exists := want[label]
		if !ok || !exists {
			return fmt.Errorf("unexpected window runner phase %q", sample.Phase)
		}
		switch kind {
		case "before":
			if active != "" || seen[label] || phaseGroup < group || phaseGroup > group+1 {
				return fmt.Errorf("duplicate, overlapping or out-of-order window runner %s", label)
			}
			active, group = label, phaseGroup
		case "during":
			if active != label {
				return fmt.Errorf("window sample does not belong to an active runner: %s", label)
			}
		case "after":
			if active != label {
				return fmt.Errorf("window END does not match its runner: %s", label)
			}
			seen[label], active = true, ""
		default:
			return fmt.Errorf("unexpected window sample phase %q", kind)
		}
	}
	if active != "" || len(seen) != len(want) {
		return errors.New("quiet window does not cover every control and measured runner")
	}
	return nil
}

type quietWindow struct {
	dir          string
	evidence     windowEvidence
	snapshot     func(string) (windowSample, error)
	journalReady bool
}

func windowSnapshot(phase string) (windowSample, error) {
	sample := windowSample{Date: time.Now().UTC().Format(time.RFC3339Nano), Phase: phase}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,pcpu=,stat=,comm=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	data, err := cmd.Output()
	if err != nil {
		return sample, fmt.Errorf("process monitoring unavailable: %w", err)
	}
	sample.Processes, err = parseProcesses(data)
	if err != nil {
		return sample, err
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		data, err = exec.CommandContext(ctx, "sysctl", "-n", "vm.loadavg").Output()
	} else {
		data, err = os.ReadFile("/proc/loadavg")
	}
	if err != nil {
		return sample, fmt.Errorf("load monitoring unavailable: %w", err)
	}
	sample.Load = strings.TrimSpace(string(data))
	return sample, nil
}

func (w *quietWindow) sample(phase string) error {
	if len(w.evidence.Samples) >= maximumWindowSamples {
		return errors.New("quiet window exceeded bounded sample count")
	}
	if err := w.initializeJournal(); err != nil {
		return err
	}
	snapshot := w.snapshot
	if snapshot == nil {
		snapshot = windowSnapshot
	}
	sample, err := snapshot(phase)
	if err != nil {
		return err
	}
	owned := ownedProcesses(sample.Processes, w.evidence.CoordinatorPID)
	for i := range sample.Processes {
		sample.Processes[i].Owned = owned[sample.Processes[i].PID]
	}
	w.evidence.Samples = append(w.evidence.Samples, sample)
	return errors.Join(w.appendSample(sample), inspectSample(sample, w.evidence.CoordinatorPID))
}

// Collection leaves a small incomplete marker and appends each compact sample
// exactly once. An interruption retains the journal, which the exact-file pass
// manifest refuses. Never rewrite the growing sample history between runners.
func (w *quietWindow) initializeJournal() error {
	if w.journalReady {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(w.dir, windowJournal), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	marker := w.evidence
	marker.Samples, marker.Ended, marker.Verdict, marker.Reason = nil, "", "INVALID", "incomplete"
	if err = w.writeEvidence(marker); err != nil {
		return err
	}
	w.journalReady = true
	return nil
}

func (w *quietWindow) appendSample(sample windowSample) error {
	data, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(w.dir, windowJournal), os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	return errors.Join(writeErr, f.Close())
}

// Write the small initial marker, or assemble the complete final evidence once
// after END/INVALID. Sync and close the replacement before publishing its name.
func (w *quietWindow) writeEvidence(evidence windowEvidence) error {
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(w.dir, "window.json")
	f, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func beginWindow(dir, receipt string) (*quietWindow, error) {
	w := &quietWindow{dir: dir, evidence: windowEvidence{Receipt: strings.TrimSpace(receipt), CoordinatorPID: os.Getpid(), Platform: runtime.GOOS, Started: time.Now().UTC().Format(time.RFC3339Nano), Verdict: "INVALID", Reason: "incomplete", IntervalMillis: int(windowInterval / time.Millisecond)}}
	var err error
	if w.evidence.Receipt == "" {
		err = errors.New("run requires -quiet-window with the operator's READY/START receipt or reference")
	} else if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		err = errors.New("quiet-window monitoring supports macOS and Linux only")
	} else if err = w.sample("preflight"); err == nil {
		err = w.sample("start")
	}
	if err != nil {
		return w, w.finish(err)
	}
	return w, nil
}

// Monitoring lives only inside this synchronous owned runner's lifetime. A
// refused sample kills this runner, never the competing process. No timers or
// background polling survive the call.
func (w *quietWindow) capture(c command, phase string) ([]byte, error) {
	if err := w.sample("before/" + phase); err != nil {
		return nil, err
	}
	cmd := exec.Command(c.args[0], c.args[1:]...)
	cmd.Dir = c.dir
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(windowInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				return output.Bytes(), err
			}
			return output.Bytes(), w.sample("after/" + phase)
		case <-ticker.C:
			if err := w.sample("during/" + phase); err != nil {
				_ = cmd.Process.Kill()
				<-done
				return output.Bytes(), err
			}
		}
	}
}

func (w *quietWindow) finish(cause error) error {
	if cause == nil {
		cause = w.sample("end")
	}
	w.evidence.Ended = time.Now().UTC().Format(time.RFC3339Nano)
	if cause == nil {
		w.evidence.Verdict, w.evidence.Reason = "OK", ""
		cause = validateWindow(w.evidence)
	}
	if cause != nil {
		w.evidence.Verdict, w.evidence.Reason = "INVALID", cause.Error()
	}
	if err := w.writeEvidence(w.evidence); err != nil {
		return errors.Join(cause, err)
	}
	// The complete final file is now persisted. Only then may the journal go;
	// any finalization or cleanup failure retains an unsealable directory.
	if w.journalReady {
		if err := os.Remove(filepath.Join(w.dir, windowJournal)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(cause, err)
		}
	}
	return cause
}
