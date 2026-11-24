package main

// The end-of-stream check (wireeof.go) against drivers written here as
// functions: no process, no clock, no socket. The sound driver reads the
// stream the way the Go leg does (test/conformance/go/wire_fuzz.go), and each
// planted driver carries one of the bugs the check exists for; every plant
// must turn exactly the cases it breaks red.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// fakeEOFDriver is a driver's stdin loop with its end-of-stream handling as
// switches. The zero value is the sound driver.
type fakeEOFDriver struct {
	partialIndexIsClose bool // 1 to 3 index bytes at EOF end the stream cleanly
	shortBodyToCodec    bool // a body cut at EOF is answered as if whole
	shortFrameExitsZero bool // a cut length or body leaves quietly with exit 0
	quietFailure        bool // a truncation exits non-zero with nothing on stderr
	cleanCloseFails     bool // the clean close is taken as an error
}

func (f fakeEOFDriver) run(stdin []byte) (wireEOFRun, error) {
	in := bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	fail := func(why string) (wireEOFRun, error) {
		if !f.quietFailure {
			errb.WriteString("driver: " + why + "\n")
		}
		return wireEOFRun{exitCode: 1, stdout: out.Bytes(), stderr: errb.Bytes()}, nil
	}
	clean := func() (wireEOFRun, error) {
		return wireEOFRun{stdout: out.Bytes(), stderr: errb.Bytes()}, nil
	}
	var count uint32
	if err := binary.Read(in, binary.LittleEndian, &count); err != nil {
		return fail("no roster")
	}
	for i := uint32(0); i < count; i++ {
		for k := 0; k < 2; k++ {
			var n uint16
			if err := binary.Read(in, binary.LittleEndian, &n); err != nil {
				return fail("roster cut")
			}
			if _, err := io.ReadFull(in, make([]byte, n)); err != nil {
				return fail("roster cut")
			}
		}
		if _, err := io.ReadFull(in, make([]byte, 2)); err != nil {
			return fail("roster cut")
		}
		out.WriteByte(1)
	}
	for {
		var index, size uint32
		if err := binary.Read(in, binary.LittleEndian, &index); err != nil {
			if errors.Is(err, io.EOF) {
				if f.cleanCloseFails {
					return fail("the stream ended")
				}
				return clean()
			}
			if f.partialIndexIsClose {
				return clean()
			}
			return fail("the stream ended inside an index")
		}
		if err := binary.Read(in, binary.LittleEndian, &size); err != nil {
			if f.shortFrameExitsZero {
				return clean()
			}
			return fail("the stream ended inside a length")
		}
		wire := make([]byte, size)
		got, err := io.ReadFull(in, wire)
		if err != nil {
			switch {
			case f.shortBodyToCodec:
				wire = wire[:got]
			case f.shortFrameExitsZero:
				return clean()
			default:
				return fail("the stream ended inside a mutant")
			}
		}
		// the reply: a 47-byte header whose saved length is the body's, then
		// the body as the saved bytes, so a cut body handed on shows as a reply
		reply := make([]byte, 47)
		binary.LittleEndian.PutUint64(reply[39:], uint64(len(wire)))
		out.Write(reply)
		out.Write(wire)
	}
}

func wireEOFTestRoots() []*wireRoot {
	return []*wireRoot{{unit: "cards", root: "Card"}, {unit: "tabledemo", root: "Wide"}}
}

func runWireEOF(t *testing.T, d fakeEOFDriver) (bool, string) {
	t.Helper()
	var log bytes.Buffer
	ok, err := wireEOFCheck(&log, wireEOFTestRoots(), d.run)
	if err != nil {
		t.Fatalf("wireEOFCheck: %v", err)
	}
	return ok, log.String()
}

func TestWireEOFSoundDriverPassesEveryCase(t *testing.T) {
	ok, log := runWireEOF(t, fakeEOFDriver{})
	if !ok {
		t.Fatalf("the sound driver failed:\n%s", log)
	}
	if got := strings.Count(log, "ok    "); got != len(wireEOFCases) {
		t.Fatalf("%d of %d cases passed:\n%s", got, len(wireEOFCases), log)
	}
}

// Every planted bug, and the cases it must turn red: no more, no fewer.
func TestWireEOFPlantedBugsGoRed(t *testing.T) {
	cuts := []string{"2 length bytes at EOF", "no body bytes at EOF", "half a body at EOF", "body one byte short at EOF"}
	bodies := []string{"no body bytes at EOF", "half a body at EOF", "body one byte short at EOF"}
	plants := []struct {
		name   string
		driver fakeEOFDriver
		red    []string
	}{
		{"partial index read as a clean close", fakeEOFDriver{partialIndexIsClose: true},
			[]string{"1 index byte at EOF", "2 index bytes at EOF", "3 index bytes at EOF"}},
		{"cut body handed to the codec", fakeEOFDriver{shortBodyToCodec: true}, bodies},
		{"cut frame leaves with exit 0", fakeEOFDriver{shortFrameExitsZero: true}, cuts},
		{"truncation fails with nothing on stderr", fakeEOFDriver{quietFailure: true},
			append([]string{"1 index byte at EOF", "2 index bytes at EOF", "3 index bytes at EOF"}, cuts...)},
		{"clean close taken as an error", fakeEOFDriver{cleanCloseFails: true}, []string{"clean close after the roster"}},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			ok, log := runWireEOF(t, p.driver)
			if ok {
				t.Fatalf("the planted driver passed:\n%s", log)
			}
			want := map[string]bool{}
			for _, r := range p.red {
				want[r] = true
			}
			for _, c := range wireEOFCases {
				red := strings.Contains(log, "FAIL  "+c.name+":")
				if red != want[c.name] {
					t.Errorf("case %q: red=%t, want red=%t\n%s", c.name, red, want[c.name], log)
				}
			}
		})
	}
}

// A leg that answers 0 for every root cannot be sent a frame: that is an
// error, never a pass.
func TestWireEOFNoKnownRootIsAnError(t *testing.T) {
	none := func(stdin []byte) (wireEOFRun, error) {
		return wireEOFRun{stdout: make([]byte, len(wireEOFTestRoots()))}, nil
	}
	var log bytes.Buffer
	if _, err := wireEOFCheck(&log, wireEOFTestRoots(), none); err == nil {
		t.Fatalf("a leg with no codec passed the check:\n%s", log.String())
	}
}

// The truncations name the entry the leg answered 1 for, not entry 0.
func TestWireEOFNamesTheFirstKnownEntry(t *testing.T) {
	var mu sync.Mutex
	var seen []uint32
	d := func(stdin []byte) (wireEOFRun, error) {
		roster := encodeWireRoster(wireEOFTestRoots())
		tail := stdin[len(roster):]
		if len(tail) >= 4 {
			mu.Lock()
			seen = append(seen, binary.LittleEndian.Uint32(tail))
			mu.Unlock()
		}
		if len(tail) == 0 {
			return wireEOFRun{stdout: []byte{0, 1}}, nil
		}
		return wireEOFRun{exitCode: 1, stdout: []byte{0, 1}, stderr: []byte("cut\n")}, nil
	}
	var log bytes.Buffer
	ok, err := wireEOFCheck(&log, wireEOFTestRoots(), d)
	if err != nil || !ok {
		t.Fatalf("ok=%t err=%v\n%s", ok, err, log.String())
	}
	if len(seen) == 0 {
		t.Fatal("no case carried a whole index")
	}
	for _, i := range seen {
		if i != 1 {
			t.Fatalf("a case named entry %d; the leg answered 1 only for entry 1", i)
		}
	}
}
