// THE WIRE-FUZZ PIPE'S END OF STREAM, asked of every leg's driver.
//
//	conformance-harness wire-eof --driver <command> [--builder|--mixed-builder]
//
// The stream (test/conformance/README.md, "The wire-fuzz driver") ends only
// on a frame boundary: the harness closes the pipe after a whole mutant, never
// inside one. A driver that reads 1 to 3 bytes of an index at EOF as a clean
// close, or hands a body cut short at EOF to its codec as if it were whole,
// turns a truncated stream into a green run. Each case below sends the roster,
// then one truncation of the stream, closes the pipe, and holds the leg to
// one outcome: the clean close exits 0 having written nothing more, and every
// truncation fails LOUDLY: a non-zero exit, a reason on stderr, and no reply
// for the cut frame.
//
// THE CASES LIVE HERE AND NOWHERE ELSE. Every leg's driver answers every one
// (make/wire-fuzz-release.mk runs this command once per leg in
// WIRE_FUZZ_RELEASE_LEGS, and once per Card-only driver); a leg is never
// handed a subset.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// wireEOFBodyLength is the length the cut frames claim; the cut ones carry
// fewer bytes than this.
const wireEOFBodyLength = 16

// wireEOFCase is one way the stream can end after the roster. tail is what is
// written after the roster (index is the roster entry the leg answered 1 for);
// clean says the leg must take it as the end of the stream.
type wireEOFCase struct {
	name  string
	clean bool
	tail  func(index uint32) []byte
}

func wireEOFIndex(index uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], index)
	return b[:]
}

func wireEOFHead(index uint32) []byte {
	return append(wireEOFIndex(index), wireEOFIndex(wireEOFBodyLength)...)
}

var wireEOFCases = []wireEOFCase{
	{name: "clean close after the roster", clean: true, tail: func(uint32) []byte { return nil }},
	{name: "1 index byte at EOF", tail: func(i uint32) []byte { return wireEOFIndex(i)[:1] }},
	{name: "2 index bytes at EOF", tail: func(i uint32) []byte { return wireEOFIndex(i)[:2] }},
	{name: "3 index bytes at EOF", tail: func(i uint32) []byte { return wireEOFIndex(i)[:3] }},
	{name: "2 length bytes at EOF", tail: func(i uint32) []byte { return wireEOFHead(i)[:6] }},
	{name: "no body bytes at EOF", tail: wireEOFHead},
	{name: "half a body at EOF", tail: func(i uint32) []byte {
		return append(wireEOFHead(i), make([]byte, wireEOFBodyLength/2)...)
	}},
	{name: "body one byte short at EOF", tail: func(i uint32) []byte {
		return append(wireEOFHead(i), make([]byte, wireEOFBodyLength-1)...)
	}},
}

// wireEOFRun is one driver process for one case: everything the harness
// wrote, and what came back.
type wireEOFRun struct {
	exitCode int
	stdout   []byte
	stderr   []byte
}

// wireEOFDriver runs the driver once on the given stdin, which it reads to
// EOF, and reports the exit, stdout and stderr. The command form runs a
// process; the unit test hands in a function, with no process at all.
type wireEOFDriver func(stdin []byte) (wireEOFRun, error)

func wireEOFCommand(command string) wireEOFDriver {
	return func(stdin []byte) (wireEOFRun, error) {
		parts := strings.Fields(command)
		if len(parts) == 0 {
			return wireEOFRun{}, fmt.Errorf("--driver names no command")
		}
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdin = bytes.NewReader(stdin)
		var out, errb bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errb
		err := cmd.Run()
		run := wireEOFRun{stdout: out.Bytes(), stderr: errb.Bytes()}
		if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				return run, fmt.Errorf("running %q: %w", command, err)
			}
			run.exitCode = ee.ExitCode()
			if run.exitCode == 0 {
				run.exitCode = -1 // killed by a signal: not a clean exit
			}
		}
		return run, nil
	}
}

// wireEOFVerdict holds one run to its case; "" is a pass.
func wireEOFVerdict(c wireEOFCase, rosterLen int, run wireEOFRun) string {
	if len(run.stdout) < rosterLen {
		return fmt.Sprintf("the leg answered %d of the %d roster bytes (exit %d): %s", len(run.stdout), rosterLen, run.exitCode, stderrLine(run.stderr))
	}
	extra := len(run.stdout) - rosterLen
	if c.clean {
		if run.exitCode != 0 {
			return fmt.Sprintf("a stream that ended on a frame boundary is a clean close, and the leg exited %d: %s", run.exitCode, stderrLine(run.stderr))
		}
		if extra != 0 {
			return fmt.Sprintf("the leg wrote %d bytes after the roster with no mutant sent", extra)
		}
		return ""
	}
	if extra != 0 {
		return fmt.Sprintf("the leg replied (%d bytes) to a frame cut short at EOF: it read the cut frame as whole", extra)
	}
	if run.exitCode == 0 {
		return "the leg exited 0: a truncated frame was read as a clean close"
	}
	if len(bytes.TrimSpace(run.stderr)) == 0 {
		return fmt.Sprintf("the leg exited %d with nothing on stderr: a truncated frame must fail with its reason", run.exitCode)
	}
	return ""
}

// stderrLine is the first line of a leg's stderr, or a note that it was empty.
func stderrLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "(nothing on stderr)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// wireEOFCheck runs every case against the driver, side by side, and writes
// one line a case. The roster is the leg's whole wire-fuzz roster, so the leg
// starts exactly as it does for a fuzz run; the truncations name the first
// entry the leg answered 1 for. It returns false when any case failed.
func wireEOFCheck(w io.Writer, roots []*wireRoot, driver wireEOFDriver) (bool, error) {
	roster := encodeWireRoster(roots)
	probe, err := driver(roster)
	if err != nil {
		return false, err
	}
	if len(probe.stdout) < len(roots) {
		return false, fmt.Errorf("the leg did not answer the %d-entry roster (exit %d): %s", len(roots), probe.exitCode, stderrLine(probe.stderr))
	}
	index := -1
	for i, b := range probe.stdout[:len(roots)] {
		if b != 0 {
			index = i
			break
		}
	}
	if index < 0 {
		return false, fmt.Errorf("the leg has a codec for none of the %d roots: no frame can name one", len(roots))
	}

	verdicts := make([]string, len(wireEOFCases))
	errs := make([]error, len(wireEOFCases))
	var wg sync.WaitGroup
	for i, c := range wireEOFCases {
		wg.Add(1)
		go func(i int, c wireEOFCase) {
			defer wg.Done()
			stdin := append(append([]byte(nil), roster...), c.tail(uint32(index))...)
			run, err := driver(stdin)
			if err != nil {
				errs[i] = err
				return
			}
			verdicts[i] = wireEOFVerdict(c, len(roots), run)
		}(i, c)
	}
	wg.Wait()

	ok := true
	var report strings.Builder
	for i, c := range wireEOFCases {
		if errs[i] != nil {
			return false, fmt.Errorf("%s: %w", c.name, errs[i])
		}
		if verdicts[i] != "" {
			ok = false
			fmt.Fprintf(&report, "FAIL  %s: %s\n", c.name, verdicts[i])
		} else {
			fmt.Fprintf(&report, "ok    %s\n", c.name)
		}
	}
	fmt.Fprintf(&report, "wire-eof: %d cases on roster entry %d (%s %s)\n", len(wireEOFCases), index, roots[index].unit, roots[index].root)
	if _, err := io.WriteString(w, report.String()); err != nil {
		return false, err
	}
	return ok, nil
}

// wireEOF is the command: the leg's roster from the manifest, as wire-fuzz
// builds it for the same profile, then every case.
func wireEOF(w io.Writer, m *Manifest, opts wireFuzzOptions) (bool, error) {
	if opts.driver == "" {
		return false, fmt.Errorf("wire-eof needs --driver")
	}
	if opts.builder && opts.mixedBuilder {
		return false, fmt.Errorf("--builder and --mixed-builder are mutually exclusive profiles")
	}
	_, roots, _, _, err := buildWireFuzzRoster(m, opts)
	if err != nil {
		return false, err
	}
	if _, err := fmt.Fprintf(w, "wire-eof: --driver %s\n", opts.driver); err != nil {
		return false, err
	}
	return wireEOFCheck(w, roots, wireEOFCommand(opts.driver))
}
