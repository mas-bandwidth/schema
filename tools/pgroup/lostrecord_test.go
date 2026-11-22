package main

import (
	"errors"
	"testing"
)

type fullDisk struct{ after int }

func (f *fullDisk) Write(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errors.New("no space left on device")
	}
	f.after--
	return len(p), nil
}

// A record write that fails never stops the run (every write reports
// success to the copier), and the first error is kept for the shim to answer
// red with at the end: a record cut short is never read as the whole run.
func TestALostRecordKeepsItsFirstError(t *testing.T) {
	l := &lostRecord{w: &fullDisk{after: 1}, path: "r.jsonl"}
	for i := range 3 {
		if n, err := l.Write([]byte("line\n")); n != 5 || err != nil {
			t.Fatalf("write %d answered %d %v: the copier would stop the run", i, n, err)
		}
	}
	if l.err == nil || l.err.Error() != "no space left on device" {
		t.Fatalf("the lost write was not kept: %v", l.err)
	}
}
