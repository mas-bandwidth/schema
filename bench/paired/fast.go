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
	"sort"
	"strconv"
	"strings"
	"time"
)

const maximumFastIterations int64 = 2147483584 // whole 64-record rotations, also fits 32-bit long
const fastMinimumSeconds = 0.2

// Uniform escalation costs a whole group per attempt, so the bound is on
// passes over the schedule, not on attempts by an individual leg.
const fastPasses = 3

type fastConfig struct {
	Rounds           int           `json:"rounds"`
	PacketIterations int64         `json:"initial_packet_iterations"`
	TableIterations  int64         `json:"initial_table_iterations"`
	Timeout          time.Duration `json:"timeout_ns"`
	Noise            string        `json:"noise_note"`
}

func (c fastConfig) validate() error {
	if c.Rounds < 1 || c.Rounds > 3 {
		return errors.New("fast-rounds must be 1..3; use run for confirmation")
	}
	for _, n := range []int64{c.PacketIterations, c.TableIterations} {
		if n <= 0 || n > maximumFastIterations || n%64 != 0 {
			return errors.New("fast iterations must be positive multiples of 64 up to 2147483584")
		}
	}
	if c.Timeout <= 0 || c.Timeout > 5*time.Minute {
		return errors.New("fast-timeout must be positive and at most 5m")
	}
	if strings.TrimSpace(c.Noise) == "" {
		return errors.New("noise-note must describe the diagnostic context")
	}
	return nil
}

func fastCommand(wire, lang string, round int, iterations int64) command {
	args := []string{"--csv", "--round", strconv.Itoa(round), "--iterations", strconv.FormatInt(iterations, 10)}
	if wire == "packet" {
		// --quick keeps the one sanctioned mixed benchmark and drops only the
		// separate bitpacker workload, which this pairing does not measure.
		args = append(args, "--quick")
	}
	return runner(wire, lang, args...)
}

func fastWires(round, languageIndex int) []string {
	if (round+languageIndex)%2 == 1 {
		return []string{"table", "packet"}
	}
	return []string{"packet", "table"}
}

// Adapt only a short sample. CSV rates encode the runner's actual measured
// duration as iterations/rate, exactly as the confirmation parser checks it.
// The 25% margin avoids aiming precisely at the 200ms acceptance boundary.
func fastNextIterations(rows []sample, current int64) (int64, bool, error) {
	adequate, fastest := true, float64(0)
	for _, row := range rows {
		adequate = adequate && float64(row.iters)/row.rate >= fastMinimumSeconds
		fastest = math.Max(fastest, row.rate)
	}
	if adequate {
		return current, true, nil
	}
	next := math.Ceil(fastest*fastMinimumSeconds*1.25/64) * 64
	if next > float64(maximumFastIterations) {
		return 0, false, errors.New("timer adequacy needs more than the supported iteration count")
	}
	return max(current+64, int64(next)), false, nil
}

// inadequateWires names the groups that never reached the floor, in the fixed
// wire order rather than the map's.
func inadequateWires(raised map[string]int64) string {
	var named []string
	for _, wire := range []string{"packet", "table"} {
		if raised[wire] != 0 {
			named = append(named, wire)
		}
	}
	return strings.Join(named, " and ")
}

// fastSchedule measures every (round, language, wire) leg and settles on ONE
// iteration count per wire. §2.1 fixes the count per benchmark, identical
// across every language, and forbids auto-scaling in a cross-language row, so
// escalation here is uniform and never per leg: when any leg of a wire's group
// falls below the 200ms floor, the count for that whole group rises to the
// largest count any of its legs needed and EVERY leg of the group is measured
// again at it. Superseded attempts stay in the evidence and supply no row, so
// a rendered table cannot mix counts. counts carries the final values out.
func fastSchedule(langs []string, rounds int, counts map[string]int64, measure func(pass, round int, lang, wire string, n int64) (int64, bool, error)) error {
	pending := map[string]bool{"packet": true, "table": true}
	for pass := range fastPasses {
		raised := map[string]int64{}
		for round := range rounds {
			for i := range langs {
				languageIndex := (i + round) % len(langs)
				lang := langs[languageIndex]
				for _, wire := range fastWires(round, languageIndex) {
					if !pending[wire] {
						continue
					}
					next, adequate, err := measure(pass, round, lang, wire, counts[wire])
					if err != nil {
						return err
					}
					if !adequate {
						raised[wire] = max(raised[wire], next)
					}
				}
			}
		}
		if len(raised) == 0 {
			return nil
		}
		if pass == fastPasses-1 {
			return fmt.Errorf("%s still has a sample below 200ms after %d uniform attempts", inadequateWires(raised), fastPasses)
		}
		pending = map[string]bool{}
		for wire, n := range raised {
			counts[wire], pending[wire] = n, true
		}
	}
	return nil
}

type fastMetric struct {
	Path       string  `json:"path"`
	Iterations int64   `json:"iterations"`
	Rate       float64 `json:"messages_per_second"`
	Seconds    float64 `json:"measured_seconds_from_csv"`
	Bytes      float64 `json:"bytes_per_op"`
}

type fastAttempt struct {
	Round      int          `json:"round"`
	Language   string       `json:"language"`
	Wire       string       `json:"wire"`
	Attempt    int          `json:"attempt"`
	Iterations int64        `json:"iterations"`
	Adequate   bool         `json:"adequate"`
	Raw        string       `json:"raw"`
	Metrics    []fastMetric `json:"metrics"`
}

type fastCommandReceipt struct {
	Arguments []string `json:"arguments"`
	Phase     string   `json:"phase"`
	Started   string   `json:"started"`
	Ended     string   `json:"ended"`
	Error     string   `json:"error,omitempty"`
}

type fastEvidence struct {
	Status        string               `json:"status"`
	Reason        string               `json:"reason,omitempty"`
	Started       string               `json:"started"`
	Ended         string               `json:"ended,omitempty"`
	Elapsed       float64              `json:"elapsed_seconds"`
	Qualification string               `json:"qualification"`
	Config        fastConfig           `json:"config"`
	FinalCounts   map[string]int64     `json:"final_iterations_per_wire,omitempty"`
	Build         buildInfo            `json:"build"`
	Noise         []string             `json:"observed_noise"`
	Commands      []fastCommandReceipt `json:"commands"`
	Attempts      []fastAttempt        `json:"attempts"`
	Artifacts     map[string]string    `json:"artifact_sha256,omitempty"`
}

type fastCollector struct {
	dir      string
	journal  *os.File
	evidence fastEvidence
}

func (f *fastCollector) save() error {
	b, err := json.MarshalIndent(f.evidence, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.dir, "fast.json"), append(b, '\n'), 0644)
}

func (f *fastCollector) snapshot(ctx context.Context, phase string) error {
	s, err := windowSnapshotContext(ctx, phase)
	if err != nil {
		return err
	}
	owned := ownedProcesses(s.Processes, os.Getpid())
	for i := range s.Processes {
		s.Processes[i].Owned = owned[s.Processes[i].PID]
	}
	// Foreign work qualifies a diagnostic; it still refuses confirmation.
	if err := inspectSample(s, os.Getpid()); err != nil {
		f.evidence.Noise = append(f.evidence.Noise, err.Error())
	}
	return json.NewEncoder(f.journal).Encode(s)
}

func (f *fastCollector) capture(ctx context.Context, c command, phase string) (output []byte, resultErr error) {
	entry := fastCommandReceipt{Arguments: c.args, Phase: phase, Started: time.Now().UTC().Format(time.RFC3339Nano)}
	defer func() {
		entry.Ended = time.Now().UTC().Format(time.RFC3339Nano)
		if resultErr != nil {
			entry.Error = resultErr.Error()
		}
		f.evidence.Commands = append(f.evidence.Commands, entry)
		resultErr = errors.Join(resultErr, f.save())
	}()
	if err := f.snapshot(ctx, "before/"+phase); err != nil {
		return nil, err
	}
	log, err := os.Create(filepath.Join(f.dir, phase+".log"))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, log.Close()) }()
	cmd := exec.CommandContext(ctx, c.args[0], c.args[1:]...)
	cmd.Dir = c.dir
	cmd.WaitDelay = time.Second
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, log
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
				return stdout.Bytes(), errors.Join(err, ctx.Err())
			}
			return stdout.Bytes(), f.snapshot(ctx, "after/"+phase)
		case <-ticker.C:
			if err := f.snapshot(ctx, "during/"+phase); err != nil {
				_ = cmd.Process.Kill() // only this owned invocation
				<-done
				return stdout.Bytes(), err
			}
		}
	}
}

func fastMeasure(langs []string, out string, info buildInfo, config fastConfig) (resultErr error) {
	if err := config.validate(); err != nil {
		return err
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return errors.New("fast process monitoring requires Linux or macOS")
	}
	if len(langs) == 4 && (info.Dirty || gitValue(".", "status", "--porcelain") != "") {
		return errors.New("fast mode requires a clean source/build checkpoint")
	}
	if out == "" {
		return errors.New("fast requires -out (a new diagnostic directory)")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		return errors.New("refusing to overwrite diagnostic directory")
	}
	for _, lang := range langs {
		for _, wire := range []string{"packet", "table"} {
			if info.Binaries[binary(wire, lang)] == "" {
				return fmt.Errorf("cached build lacks %s/%s; run the gate first", lang, wire)
			}
		}
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(out), ".paired-fast-")
	if err != nil {
		return err
	}
	f := fastCollector{dir: tmp, evidence: fastEvidence{Status: "INCOMPLETE_FAST_DIAGNOSTIC", Started: started.UTC().Format(time.RFC3339Nano), Config: config, Build: info, Qualification: "Non-certified diagnostic: reduced iterations and 1..3 rounds; no bracketing drift controls or quiet-window seal. Process samples can miss short or unusually named work. One warmup per path is retained at the requested sample count, half the standard count at the defaults, so managed-runtime steady state still needs confirmation."}}
	f.journal, err = os.Create(filepath.Join(tmp, "process-samples.jsonl"))
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "fast diagnostic evidence:", tmp)
	defer func() {
		resultErr = errors.Join(resultErr, f.journal.Close())
		resultErr = errors.Join(resultErr, ctx.Err())
		f.evidence.Ended, f.evidence.Elapsed = time.Now().UTC().Format(time.RFC3339Nano), time.Since(started).Seconds()
		if resultErr != nil {
			f.evidence.Status = "INCOMPLETE_FAST_DIAGNOSTIC"
			f.evidence.Reason = resultErr.Error()
		}
		resultErr = errors.Join(resultErr, f.save())
		if resultErr == nil {
			resultErr = os.Rename(tmp, out)
			if resultErr == nil {
				fmt.Fprintln(os.Stderr, "completed non-certified fast diagnostic:", out)
			}
		}
	}()
	if err := f.save(); err != nil {
		return err
	}
	// These are the same no-clock correctness gates as the confirmation pass.
	if _, err := f.capture(ctx, command{args: []string{"build/paired/corpus", "verify"}}, "corpus-gate"); err != nil {
		return err
	}
	for _, lang := range langs {
		for _, wire := range []string{"packet", "table"} {
			data, err := f.capture(ctx, runner(wire, lang, "--gate"), "gate-"+lang+"-"+wire)
			if err != nil {
				return err
			}
			if len(bytes.TrimSpace(data)) != 0 {
				return errors.New("correctness gate emitted timing rows")
			}
		}
	}
	counts := map[string]int64{"packet": config.PacketIterations, "table": config.TableIterations}
	err = fastSchedule(langs, config.Rounds, counts, func(pass, round int, lang, wire string, n int64) (int64, bool, error) {
		phase := fmt.Sprintf("round-%d-%s-%s-attempt-%d", round, lang, wire, pass)
		fmt.Fprintln(os.Stderr, phase, "iterations", n)
		data, runErr := f.capture(ctx, fastCommand(wire, lang, round, n), phase)
		if err := os.WriteFile(filepath.Join(tmp, phase+".csv"), data, 0644); err != nil {
			return 0, false, errors.Join(runErr, err)
		}
		if runErr != nil {
			return 0, false, runErr
		}
		rows, err := parseRowsForIterations(data, lang, wire, info.CorpusIDs[wire], n, 0)
		if err != nil {
			return 0, false, err
		}
		next, adequate, err := fastNextIterations(rows, n)
		if err != nil {
			return 0, false, err
		}
		a := fastAttempt{Round: round, Language: lang, Wire: wire, Attempt: pass, Iterations: n, Adequate: adequate, Raw: phase + ".csv"}
		for _, row := range rows {
			a.Metrics = append(a.Metrics, fastMetric{Path: row.cols[2], Iterations: row.iters, Rate: row.rate, Seconds: float64(row.iters) / row.rate, Bytes: row.bytes})
		}
		f.evidence.Attempts = append(f.evidence.Attempts, a)
		return next, adequate, f.save()
	})
	if err != nil {
		return err
	}
	f.evidence.FinalCounts = counts
	if err := f.save(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkBuildRevision(info.Revision, gitValue(".", "rev-parse", "HEAD")); err != nil {
		return err
	}
	if err := verifyRecordedArtifacts(info); err != nil {
		return err
	}
	// The owned diagnostic directory may live outside ignored build/. It is
	// evidence, not a source change; every other tracked/untracked path counts.
	statusArgs := []string{"status", "--porcelain", "--", "."}
	if rel, err := filepath.Rel(abs("."), abs(tmp)); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		statusArgs = append(statusArgs, ":(exclude,top,literal)"+filepath.ToSlash(rel))
	}
	if len(langs) == 4 && gitValue(".", statusArgs...) != "" {
		return errors.New("source changed during fast diagnostic")
	}
	if err := writeFastSummary(tmp, langs, f.evidence); err != nil {
		return err
	}
	f.evidence.Artifacts = map[string]string{}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "fast.json" {
			continue
		}
		hash, err := hashFile(filepath.Join(tmp, entry.Name()))
		if err != nil {
			return err
		}
		f.evidence.Artifacts[entry.Name()] = hash
	}
	f.evidence.Status = "COMPLETED_NON_CERTIFIED_FAST_DIAGNOSTIC"
	return nil
}

// grouped writes an iteration count the way the prose does, 2,000,000 rather
// than 2000000, so the stated count reads as the same number in both places.
func grouped(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// langs is the set actually measured, which is not always every paired
// language: a leg whose rows do not answer to this sitting's corpus is left
// out before the first clock rather than refused after it.
func writeFastSummary(dir string, langs []string, e fastEvidence) error {
	costs := map[string][]float64{}
	for _, attempt := range e.Attempts {
		// Only the final uniform count supplies rows; a superseded attempt is
		// evidence of the escalation, not a measurement of this table.
		if !attempt.Adequate || attempt.Iterations != e.FinalCounts[attempt.Wire] {
			continue
		}
		for _, m := range attempt.Metrics {
			key := attempt.Language + "/" + attempt.Wire + "/" + m.Path
			costs[key] = append(costs[key], 1e6/m.Rate)
		}
	}
	median := map[string]float64{}
	var text strings.Builder
	fmt.Fprintf(&text, "# Fast iteration diagnostic — not certified\n\n%s\n\nOperator context: %s\n\n%d observed noise warnings; see fast.json and process-samples.jsonl. Every accepted sample is at least 200ms. Short attempts are retained but excluded below. Costs use the median of the adequate rounds; one round does not establish stability.\n\nOne iteration count per wire, identical across all four languages and every round (BENCH-STANDARD §2.1): %s Packet and %s Table operations per warmup and per measured sample. A short leg raised the count for its whole group, never for itself alone. At one round the Range column is degenerate — the single value is its own minimum and maximum — so it reads as zero variance by construction, not as measured stability.\n\n| Language | Wire | Path | Median µs/op | Range µs/op |\n|---|---|---|---:|---:|\n", e.Qualification, e.Config.Noise, len(e.Noise), grouped(e.FinalCounts["packet"]), grouped(e.FinalCounts["table"]))
	for _, lang := range langs {
		for _, wire := range []string{"packet", "table"} {
			for _, path := range []string{"write", "round_trip"} {
				key := lang + "/" + wire + "/" + path
				values := costs[key]
				if len(values) != e.Config.Rounds {
					return fmt.Errorf("incomplete adequate fast pairs: %s", key)
				}
				sort.Float64s(values)
				middle := len(values) / 2
				v := values[middle]
				if len(values)%2 == 0 {
					v = (v + values[middle-1]) / 2
				}
				median[key] = v
				fmt.Fprintf(&text, "| %s | %s | %s | %.3f | %.3f–%.3f |\n", names[lang], wire, path, v, values[0], values[len(values)-1])
			}
		}
	}
	fastest := math.Inf(1)
	for _, lang := range langs {
		fastest = math.Min(fastest, median[lang+"/table/round_trip"])
	}
	text.WriteString("\n| Language | Fixed Table % | vs Packet Wire % |\n|---|---:|---:|\n")
	for _, lang := range langs {
		table := median[lang+"/table/round_trip"]
		fmt.Fprintf(&text, "| %s | %.1f%% | %.1f%% |\n", names[lang], 100*table/fastest, 100*table/median[lang+"/packet/round_trip"])
	}
	text.WriteString("\n" + checksDetails + "\n\nConfirmation remains the separate seven-round `-mode run` protocol. This directory has no confirmation seal.\n")
	fmt.Print(text.String())
	return os.WriteFile(filepath.Join(dir, "README.md"), []byte(text.String()), 0644)
}
