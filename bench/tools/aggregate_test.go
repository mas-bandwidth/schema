package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A round file as the pass driver writes one: the runner's CSV with the
// driver's `# round: K` stamp in the preamble and one measured run per row.
func writeRound(t *testing.T, dir, name, round string, rows ...string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	body := "# schema bench results\n# round: " + round + "\n" +
		"lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" +
		strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runTool builds the tool into the test's temp dir, once per call, and runs
// that binary, so the exit status observed is the tool's own — `go run` would
// report the tool's exit 2 as its own exit 1.
func runTool(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bench-tools")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, args...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

// The output loop of aggregate walks the package-level `paths` list (write,
// read, round_trip). Until 2026-09-07 the function's own parameter was also
// named `paths` and shadowed it, so the loop walked the input FILE names,
// printed no row, and the straggler refusal fired on every key of every
// pass with the message that the order list did not know the bench. The
// driver had not run a pass since the loop was written, so nothing noticed.
// This test is the pass the driver would have run: two rounds, the current
// corpus's rows, and every row must come out aggregated.
func TestAggregatePrintsEveryRowOfACurrentCorpusPass(t *testing.T) {
	dir := t.TempDir()
	r0 := writeRound(t, dir, "round-0-cpp.csv", "0",
		"cpp,bench_mixed,write,4000000,100,1,9000000,9000000,9000000,3759.77,0.00,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown",
		"cpp,bench_mixed,round_trip,4000000,100,1,4500000,4500000,4500000,1879.88,0.00,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown",
		"cpp,bitpacker,write,24576,65536,1,95000,95000,95000,5937.50,0.00,6b213fbfa1a03a99,bits,hdr,removed,O3,unknown",
		"cpp,bitpacker,read,24576,65536,1,125000,125000,125000,7812.50,0.00,6b213fbfa1a03a99,bits,hdr,removed,O3,unknown")
	r1 := writeRound(t, dir, "round-1-cpp.csv", "1",
		"cpp,bench_mixed,write,4000000,100,1,9200000,9200000,9200000,3843.32,0.00,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown",
		"cpp,bench_mixed,round_trip,4000000,100,1,4400000,4400000,4400000,1838.11,0.00,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown",
		"cpp,bitpacker,write,24576,65536,1,97000,97000,97000,6062.50,0.00,6b213fbfa1a03a99,bits,hdr,removed,O3,unknown",
		"cpp,bitpacker,read,24576,65536,1,123000,123000,123000,7687.50,0.00,6b213fbfa1a03a99,bits,hdr,removed,O3,unknown")

	out, errOut, code := runTool(t, "aggregate", r0, r1)
	if code != 0 {
		t.Fatalf("aggregate refused a well-formed two-round pass (exit %d):\n%s", code, errOut)
	}
	for _, want := range []string{
		"cpp,bench_mixed,write,4000000,100,2,9100000,9000000,9200000,",
		"cpp,bench_mixed,round_trip,4000000,100,2,4450000,4400000,4500000,",
		"cpp,bitpacker,write,24576,65536,2,96000,95000,97000,",
		"cpp,bitpacker,read,24576,65536,2,124000,123000,125000,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("aggregated output lacks the row %q\n--- stdout ---\n%s--- stderr ---\n%s", want, out, errOut)
		}
	}
}

// The codegen-only legs (java, dart, elixir) run in every pass the driver
// drives — bench/run.sh has run them since they landed — but they were
// missing from `langs`, which is aggregate's OUTPUT loop. Their rows would
// have accumulated and never printed, and the straggler refusal (the #689
// class) would have taken the whole pass down with them. This test is the
// pass that carries all three: every row must come out aggregated.
func TestAggregatePrintsTheCodegenOnlyLegs(t *testing.T) {
	dir := t.TempDir()
	r0 := writeRound(t, dir, "round-0-codegen.csv", "0",
		"java,bench_mixed,write,4000000,100,1,5800000,5800000,5800000,553.13,0.00,6b213fbfa1a03a99,gen,class,contract,default,unknown",
		"java,bench_mixed,round_trip,4000000,100,1,2200000,2200000,2200000,209.81,0.00,6b213fbfa1a03a99,gen,class,contract,default,unknown",
		"dart,bench_mixed,write,4000000,100,1,2800000,2800000,2800000,267.03,0.00,6b213fbfa1a03a99,gen,aot,contract,default,unknown",
		"dart,bench_mixed,round_trip,4000000,100,1,1580000,1580000,1580000,150.68,0.00,6b213fbfa1a03a99,gen,aot,contract,default,unknown",
		"elixir,bench_mixed,write,4000000,100,1,450000,450000,450000,42.92,0.00,6b213fbfa1a03a99,gen,beam,contract,default,unknown",
		"elixir,bench_mixed,round_trip,4000000,100,1,280000,280000,280000,26.70,0.00,6b213fbfa1a03a99,gen,beam,contract,default,unknown")
	r1 := writeRound(t, dir, "round-1-codegen.csv", "1",
		"java,bench_mixed,write,4000000,100,1,5900000,5900000,5900000,562.67,0.00,6b213fbfa1a03a99,gen,class,contract,default,unknown",
		"java,bench_mixed,round_trip,4000000,100,1,2300000,2300000,2300000,219.35,0.00,6b213fbfa1a03a99,gen,class,contract,default,unknown",
		"dart,bench_mixed,write,4000000,100,1,2900000,2900000,2900000,276.57,0.00,6b213fbfa1a03a99,gen,aot,contract,default,unknown",
		"dart,bench_mixed,round_trip,4000000,100,1,1600000,1600000,1600000,152.59,0.00,6b213fbfa1a03a99,gen,aot,contract,default,unknown",
		"elixir,bench_mixed,write,4000000,100,1,460000,460000,460000,43.87,0.00,6b213fbfa1a03a99,gen,beam,contract,default,unknown",
		"elixir,bench_mixed,round_trip,4000000,100,1,300000,300000,300000,28.61,0.00,6b213fbfa1a03a99,gen,beam,contract,default,unknown")

	out, errOut, code := runTool(t, "aggregate", r0, r1)
	if code != 0 {
		t.Fatalf("aggregate refused a pass carrying the codegen-only legs (exit %d):\n%s", code, errOut)
	}
	for _, want := range []string{
		"java,bench_mixed,write,4000000,100,2,5850000,5800000,5900000,",
		"java,bench_mixed,round_trip,4000000,100,2,2250000,2200000,2300000,",
		"dart,bench_mixed,write,4000000,100,2,2850000,2800000,2900000,",
		"dart,bench_mixed,round_trip,4000000,100,2,1590000,1580000,1600000,",
		"elixir,bench_mixed,write,4000000,100,2,455000,450000,460000,",
		"elixir,bench_mixed,round_trip,4000000,100,2,290000,280000,300000,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("aggregated output lacks the row %q\n--- stdout ---\n%s--- stderr ---\n%s", want, out, errOut)
		}
	}
}

// The negative control: the straggler refusal must still fire on a path the
// column list does not know, so a runner that grows a new path REFUSES the
// aggregation rather than losing the row (BENCH-STANDARD §5.1).
func TestAggregateRefusesAPathTheListDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	r0 := writeRound(t, dir, "round-0-cpp.csv", "0",
		"cpp,bench_mixed,sideways,4000000,100,1,9000000,9000000,9000000,3759.77,0.00,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown")
	out, errOut, code := runTool(t, "aggregate", r0)
	if code != 2 {
		t.Fatalf("aggregate accepted a row on an unknown path (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "accumulated but not printed") || !strings.Contains(errOut, "cpp/bench_mixed/sideways") {
		t.Errorf("the straggler refusal did not fire naming the row:\n%s", errOut)
	}
}

// bands reads its PRIOR files from its second argument onward. The same
// shadow class that broke aggregate was recreated here by the first rename
// (the loop kept reading `paths[1:]`, which had become the column list), so
// the command opened a file named `read`. Two files, one band, must print
// band-ok.
func TestBandsReadsThePriorFilesItIsGiven(t *testing.T) {
	dir := t.TempDir()
	cur := writeRound(t, dir, "current.csv", "0",
		"cpp,bench_mixed,write,4000000,100,7,9000000,8900000,9100000,858.31,2.22,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown")
	prior := writeRound(t, dir, "prior.csv", "0",
		"cpp,bench_mixed,write,4000000,100,7,9050000,8950000,9150000,863.08,2.21,6b213fbfa1a03a99,gen,hdr,removed,O3,unknown")
	out, errOut, code := runTool(t, "bands", cur, prior)
	if code != 0 || !strings.Contains(out, "band-ok: cpp/bench_mixed/write") {
		t.Fatalf("bands did not band the current row against its prior (exit %d)\n--- stdout ---\n%s--- stderr ---\n%s", code, out, errOut)
	}
}

// controlmedian's refusal names the FILE it found no control rows in; after
// the first rename it named the first path-column value instead.
func TestControlMedianRefusalNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	bits := writeRound(t, dir, "bits-only.csv", "0",
		"cpp,bitpacker,write,24576,4096,7,95000,94000,96000,371.09,2.11,6b213fbfa1a03a99,bits,hdr,removed,O3,unknown")
	_, errOut, code := runTool(t, "controlmedian", bits)
	if code != 2 || !strings.Contains(errOut, "bits-only.csv") {
		t.Fatalf("controlmedian did not refuse naming the file (exit %d):\n%s", code, errOut)
	}
}
