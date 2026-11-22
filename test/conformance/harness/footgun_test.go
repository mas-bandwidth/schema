// NOTHING IS ALLOWED TO FAIL SILENTLY (Glenn, 2026-11-22: "we gotta remove
// all these silly footguns"). The spec-only Zig port (PR #4726) ran a driver
// that answered nothing, and the harness printed "every registered surface
// passes". These tests plant each way a driver can say less than it was asked
// and hold the run to a loud, named failure — or, where the contract allows
// an absence, to a summary that does not call it a pass.
//
// The driver is THIS TEST BINARY (TestMain dispatches on fakeDriverEnv), so
// the tests need no shell, no toolchain, no socket and no clock: they run in
// the fast tier, not behind SCHEMA_SLOW.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeDriverEnv names the fake driver's mode; fakeJSONEnv the JSON directory
// its json-write surface copies from.
const (
	fakeDriverEnv = "SCHEMA_HARNESS_FAKE_DRIVER"
	fakeJSONEnv   = "SCHEMA_HARNESS_FAKE_JSON"
)

// fakeDriver is the driver protocol (README.md) answered from the derived
// manifest, broken in the way mode names. It returns the exit code.
//
//	good            answers wire, json-read and json-write over every instance
//	silent          `list` prints nothing and exits 0
//	unknown-surface `list` also names a surface the harness does not have
//	partial         wire leaves beta unanswered
//	ghost           wire also answers a row named ghost
//	malformed       wire answers alpha with bytes that are not its wire
//	crash-signal    wire answers alpha, then dies on SIGKILL
//	crash-panic     wire answers alpha, then panics (a Go panic exits 2)
//	all-absent      every surface it lists exits 2
//	all-case-absent every case of every surface is `<case>.absent`
//	beta-absent     wire answers beta `.absent`, the rest as good
//	wire-only       `list` names wire alone
func fakeDriver(mode string, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "fake driver: usage: <manifest> <surface> [outdir]")
		return 1
	}
	manifest, surface := args[0], args[1]
	if surface == "list" {
		switch mode {
		case "silent":
			return 0
		case "wire-only":
			fmt.Println("wire")
			return 0
		case "unknown-surface":
			fmt.Println("wirez")
		}
		fmt.Println("wire\njson-read\njson-write")
		return 0
	}
	if mode == "all-absent" {
		return 2
	}
	out := args[2]
	instances, err := fakeInstances(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for i, inst := range instances {
		name, wire := inst[0], inst[1]
		if mode == "all-case-absent" || (mode == "beta-absent" && surface == "wire" && name == "beta") {
			if err := os.WriteFile(filepath.Join(out, fakeAnswerName(surface, name)+".absent"), nil, 0o644); err != nil {
				return 1
			}
			continue
		}
		if surface == "wire" && i == 1 {
			switch mode {
			case "partial":
				continue
			case "crash-signal":
				if p, err := os.FindProcess(os.Getpid()); err == nil {
					_ = p.Kill()
				}
				select {} // the kill lands; nothing after it runs
			case "crash-panic":
				panic("fake driver: mid-stream")
			}
		}
		var body []byte
		switch {
		case surface == "json-write":
			body, err = os.ReadFile(filepath.Join(os.Getenv(fakeJSONEnv), name+".json"))
		case mode == "malformed" && surface == "wire" && name == "alpha":
			body = []byte("not a wire")
		default:
			body, err = os.ReadFile(wire)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, fakeAnswerName(surface, name)), body, 0o644); err != nil {
			return 1
		}
	}
	if mode == "ghost" && surface == "wire" {
		if err := os.WriteFile(filepath.Join(out, "ghost"), []byte("x"), 0o644); err != nil {
			return 1
		}
	}
	return 0
}

// fakeAnswerName is the file a case's answer goes in on surface.
func fakeAnswerName(surface, name string) string {
	if surface == "json-write" {
		return name + ".json"
	}
	return name
}

// fakeInstances reads (name, wire path) for every `instance` line of the
// derived manifest.
func fakeInstances(manifest string) ([][2]string, error) {
	f, err := os.Open(manifest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out [][2]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "instance" {
			out = append(out, [2]string{fields[1], fields[4]})
		}
	}
	return out, sc.Err()
}

// twoRowCorpus is a corpus of two instances, alpha and beta, so a driver can
// answer one row of a surface and not the other.
func twoRowCorpus(t *testing.T) fakeCorpus {
	t.Helper()
	dir := t.TempDir()
	c := fakeCorpus{
		manifest: filepath.Join(dir, "MANIFEST.txt"),
		jsonDir:  filepath.Join(dir, "json"),
		reports:  filepath.Join(dir, "reports.txt"),
		wire:     filepath.Join(dir, "row.bin"),
	}
	writeFile(t, c.wire, "\x01\x02\x03\x04")
	writeFile(t, filepath.Join(c.jsonDir, "alpha.json"), "{\"alpha\":1}\n")
	writeFile(t, filepath.Join(c.jsonDir, "beta.json"), "{\"beta\":2}\n")
	writeFile(t, c.manifest, "unit u tables/examples\ninstance alpha u Root row.bin\ninstance beta u Root row.bin\n")
	writeFile(t, c.reports, "")
	m, err := ReadManifest(c.manifest, c.jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Instances {
		m.Instances[i].Wire = c.wire
	}
	c.m = m
	return c
}

// runFake runs the harness over the two-row corpus with this binary as the
// one driver of a substituted registry, in mode, restricted to only when
// non-empty. strictRows is on, as main.go runs it.
func runFake(t *testing.T, mode string, only string) (string, bool, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(exe, " \t") {
		t.Skipf("the test binary's path %q has whitespace, which a registry line cannot carry", exe)
	}
	c := twoRowCorpus(t)
	t.Setenv(fakeDriverEnv, mode)
	t.Setenv(fakeJSONEnv, c.jsonDir)
	drivers := filepath.Join(t.TempDir(), "drivers.txt")
	writeFile(t, drivers, "go "+exe+"\n")
	opts := runOptions{strictRows: true}
	if only != "" {
		set, err := parseSurfaces(only)
		if err != nil {
			t.Fatal(err)
		}
		opts.only = set
	}
	var out bytes.Buffer
	ok, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		filepath.Join(t.TempDir(), "work"), "", "", opts)
	return out.String(), ok, err
}

// mustFail requires a red run whose output names every one of says.
func mustFail(t *testing.T, mode, only string, says ...string) {
	t.Helper()
	out, ok, err := runFake(t, mode, only)
	if err != nil {
		t.Fatalf("mode %s: the run errored instead of scoring: %v\n%s", mode, err, out)
	}
	if ok {
		t.Fatalf("mode %s: the run stayed GREEN:\n%s", mode, out)
	}
	mustSay(t, out, says, []string{footer, notAllAnswered})
}

// TestFakeDriverGoodIsGreen is the positive control every red below is read
// against: the same driver, unbroken, passes every row and prints the footer.
func TestFakeDriverGoodIsGreen(t *testing.T) {
	out, ok, err := runFake(t, "good", "")
	if err != nil || !ok {
		t.Fatalf("the unbroken fake driver did not pass: %v\n%s", err, out)
	}
	mustSay(t, out, []string{footer, "pass 2/2"}, []string{"FAIL", notAllAnswered})
}

// TestDriverListingNothingIsRed: exit 0 and no output from `list` (the Zig
// case). Before: every surface printed `absent` over the success footer.
func TestDriverListingNothingIsRed(t *testing.T) {
	out, _, err := runFake(t, "silent", "")
	if err == nil || !strings.Contains(err.Error(), "named no surface") || !strings.Contains(err.Error(), "go: ") {
		t.Fatalf("a driver whose `list` named nothing was not refused by leg: %v\n%s", err, out)
	}
}

// TestDriverListingAnUnknownSurfaceIsRed: a name in `list` the harness has no
// surface for was dropped in silence.
func TestDriverListingAnUnknownSurfaceIsRed(t *testing.T) {
	out, _, err := runFake(t, "unknown-surface", "")
	if err == nil || !strings.Contains(err.Error(), "wirez") {
		t.Fatalf("a driver listing the unknown surface wirez was not refused by name: %v\n%s", err, out)
	}
}

// TestDriverAnsweringSomeRowsIsRed: a row left unanswered fails by name.
func TestDriverAnsweringSomeRowsIsRed(t *testing.T) {
	mustFail(t, "partial", "", "go / wire: beta: the driver wrote nothing", "FAIL 1/2")
}

// TestDriverAnsweringAGhostRowIsRed: an answer to a row the corpus did not
// send was never read. Before: the run was green.
func TestDriverAnsweringAGhostRowIsRed(t *testing.T) {
	mustFail(t, "ghost", "", "go / wire: ghost: the driver answered a row the corpus did not send")
}

// TestDriverMalformedAnswerIsRed: an answer that is not the row's bytes fails
// by row and says where.
func TestDriverMalformedAnswerIsRed(t *testing.T) {
	mustFail(t, "malformed", "", "go / wire: alpha: 10 bytes out, 4 expected")
}

// TestDriverCrashingMidStreamIsRed: a driver that dies after one answer fails
// the surface, says how it died, and names the rows it left. A Go panic exits
// 2, the absent code: before, it read as an ordinary absence.
func TestDriverCrashingMidStreamIsRed(t *testing.T) {
	signal := "go / wire: the driver was killed by a signal"
	if runtime.GOOS == "windows" {
		signal = "go / wire: the driver exited 1" // TerminateProcess has no signal
	}
	mustFail(t, "crash-signal", "", signal, "1 of 2 row(s) unanswered: beta")
	mustFail(t, "crash-panic", "", "go / wire: the driver exited 2 (absent) but it had already written 1 answer file(s): a crash, not an absence")
}

// TestLegAnsweringNothingIsRed: a leg whose every surface is absent, or whose
// every case is `.absent`, answered nothing. Before: green, `absent` cells.
func TestLegAnsweringNothingIsRed(t *testing.T) {
	mustFail(t, "all-absent", "", "go / (every surface): the leg answered none of the 6 row(s) sent on 3 surface(s)")
	mustFail(t, "all-case-absent", "", "go / (every surface): the leg answered none of the 6 row(s) sent on 3 surface(s)")
}

// TestSurfacesFilterMatchingNothingIsRed: --surfaces naming only surfaces the
// leg does not run asks it nothing, and a run that asked nothing is not green.
// --only naming no leg is refused outright.
func TestSurfacesFilterMatchingNothingIsRed(t *testing.T) {
	mustFail(t, "wire-only", "json-write", "go / (every surface): the leg answered none of the 2 row(s) sent on 1 surface(s)")

	c := twoRowCorpus(t)
	registry := filepath.Join(t.TempDir(), "drivers.txt")
	writeFile(t, registry, "go /nonexistent/driver\n")
	var out bytes.Buffer
	_, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, registry,
		filepath.Join(t.TempDir(), "work"), "nosuchleg", "", runOptions{strictRows: true})
	if err == nil || !strings.Contains(err.Error(), "conformance ran no leg") {
		t.Fatalf("a run whose --only matched no leg was not refused: %v\n%s", err, out.String())
	}
}

// TestSummaryNeverCallsAnAbsenceAPass: a declared per-case absence stays a
// port's completion tracker (not red), but the cell's denominator is every
// row sent and the footer names what was not answered instead of the success
// line. Before: "pass 1/1 +1a" and "every registered surface passes".
func TestSummaryNeverCallsAnAbsenceAPass(t *testing.T) {
	out, ok, err := runFake(t, "beta-absent", "")
	if err != nil || !ok {
		t.Fatalf("a declared per-case absence turned the run red: %v\n%s", err, out)
	}
	mustSay(t, out,
		[]string{"pass 1/2 +1a", notAllAnswered + " — absent is not a pass:", "go / wire: 1 of 2 row(s) ABSENT"},
		[]string{footer, "pass 1/1 +1a"})
}
