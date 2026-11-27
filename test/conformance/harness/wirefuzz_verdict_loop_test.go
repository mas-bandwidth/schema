package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE WIRE-FUZZ LOOP JUDGES EVERY MUTANT (a fast judge of the harness's own
// verdict, run with the harness package's tests in the nightly tier). The
// verdict helpers have unit tests, but nothing drove the loop that calls
// them: a harness edit that dropped the verdict (`verdict = ""` after
// wireVerdict) went green, and only a leg's wire-fuzz negative control could
// see it, after building that leg's generated tree. This test drives the
// whole loop in-process instead: wireFuzz over one pinned seed, against a
// leg that refuses everything. The oracle accepts the seed, so the run must fail on
// the first mutant and name the difference. The control is the same leg
// answering a seed the oracle refuses too, which must pass.
//
// The leg is this test binary, re-run with refusingLegEnv set and entered
// from TestMain (refusingWireLeg): a child process on pipes, no socket and no clock (the run's own --timeout is
// 0, and the reply deadline never fires on a leg that answers at once).
func TestWireFuzzLoopIsRedOnADivergingLeg(t *testing.T) {
	_, _, _ = corpus(t) // the repository root, where the manifest's paths resolve
	t.Setenv(refusingLegEnv, "1")
	driver := os.Args[0]

	dir := t.TempDir()
	run := func(name, wire string) (string, error) {
		t.Helper()
		manifest := filepath.Join(dir, name+".MANIFEST.txt")
		text := "unit blobdemo tables/blobs\ninstance " + name + " blobdemo Catalog " + wire + "\n"
		if err := os.WriteFile(manifest, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := ReadManifest(manifest, "")
		if err != nil {
			t.Fatal(err)
		}
		failed := filepath.Join(dir, name+"-failed.bin")
		return failed, wireFuzz(m, wireFuzzOptions{driver: driver, maxMutants: 1, failed: failed, leg: "refusing", ledgerDir: dir}) // a named leg with no ledger (W-ROSTER)
	}

	// the control: a seed the oracle refuses as well, so the leg's refusal
	// agrees and the run passes (the leg itself is sound)
	garbage := filepath.Join(dir, "garbage.bin")
	if err := os.WriteFile(garbage, []byte{0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run("refused_by_both", garbage); err != nil {
		t.Fatalf("control: the refusing leg and the oracle agree on a refused seed, and the run failed:\n%v", err)
	}

	failed, err := run("blob_small", "testdata/wire/tables/blob_small.bin")
	if err == nil {
		t.Fatal("a leg that refused a seed the oracle accepts passed wire-fuzz: the loop dropped its verdict")
	}
	if !strings.Contains(err.Error(), "read acceptance differs: the leg says false, the oracle says true") {
		t.Fatalf("the failure does not name the acceptance difference:\n%v", err)
	}
	if _, statErr := os.Stat(failed); statErr != nil {
		t.Errorf("the diverging mutant was not written for replay: %v", statErr)
	}
}

// refusingLegEnv turns this test binary into the wire-fuzz leg the test above
// drives: TestMain hands the process to refusingWireLeg before any test runs.
const refusingLegEnv = "SCHEMA_HARNESS_REFUSING_WIRE_LEG"

// refusingWireLeg is the leg: it names every root in the roster as known and
// answers every mutant with a malformed refusal and no write-back, until its
// input closes. It is entered from TestMain, not written as a Test, so no
// ordinary run carries a helper's skip, and it exits the process itself.
func refusingWireLeg() {
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	var n uint32
	if err := binary.Read(in, binary.LittleEndian, &n); err != nil {
		os.Exit(3)
	}
	for range n {
		for range 2 { // the unit and the root, each a u16-length string
			var l uint16
			if err := binary.Read(in, binary.LittleEndian, &l); err != nil {
				os.Exit(3)
			}
			if _, err := io.CopyN(io.Discard, in, int64(l)); err != nil {
				os.Exit(3)
			}
		}
		if _, err := io.CopyN(io.Discard, in, 2); err != nil { // form, retain
			os.Exit(3)
		}
	}
	for range n {
		_ = out.WriteByte(1)
	}
	if out.Flush() != nil {
		os.Exit(3)
	}
	// the reply head receive() reads: loaded, five counters, malformed,
	// refused, measure, retained, retain-lost, then the saved length (-1: none)
	var reply [1 + 5*4 + 1 + 1 + 8 + 8 + 4 + 4]byte
	reply[21] = 1 // malformed
	binary.LittleEndian.PutUint64(reply[39:], ^uint64(0))
	for {
		var head [8]byte
		if _, err := io.ReadFull(in, head[:]); err != nil {
			os.Exit(0) // the harness closed the stream
		}
		if _, err := io.CopyN(io.Discard, in, int64(binary.LittleEndian.Uint32(head[4:]))); err != nil {
			os.Exit(3)
		}
		if _, err := out.Write(reply[:]); err != nil || out.Flush() != nil {
			os.Exit(0) // the harness killed the stream on a failure
		}
	}
}
