// A SURFACE THAT RAN NO ROWS IS A FAILURE, NOT A PASS (Glenn, 2026-11-22:
// "remove footguns"). A leg that lists a surface and is handed a corpus with no
// rows for it would print `pass 0/0`, a green cell over a comparison of
// nothing. The gate (main.go) runs with strictRows, which turns that cell red.
package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestSurfaceWithNoRowsIsRed(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t) // rows for wire, json-read and json-write only
	drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}})
	var out bytes.Buffer
	ok, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		filepath.Join(t.TempDir(), "work"), "", "", runOptions{strictRows: true})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("the leg listed surfaces the corpus gave no rows and the gate stayed green:\n%s", out.String())
	}
	mustSay(t, out.String(), []string{"corpus gave it 0 rows to run", "cpp / block: the leg lists the block surface"}, []string{footer})
	// the same run without strictRows is the fake corpus's own shape and stays green
	out.Reset()
	ok, err = runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		filepath.Join(t.TempDir(), "work2"), "", "", runOptions{})
	if err != nil || !ok {
		t.Fatalf("the default run turned red: %v\n%s", err, out.String())
	}
}
