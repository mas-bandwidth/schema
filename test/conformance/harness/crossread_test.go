// THE SECOND DIRECTION (test/conformance/README.md, "The nightly"): the C++
// reference reads what a port wrote. A fake reference driver over the
// one-instance corpus absence_test.go builds stands in for the C++ leg, so each
// way the direction can fail is gated without a language leg: the port's bytes
// are not C++'s, C++ refuses them, C++ re-saves them differently. No network,
// no clock.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeReference plants a committed registry whose reference re-reads by
// copying each line's file to its answer — a faithful round trip — except
// that it exits 1 when the manifest names refuse, and appends a byte to every
// answer when alter is set.
func fakeReference(t *testing.T, refuse string, alter bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "registry")
	tail := ""
	if alter {
		tail = `printf x >> "$dest"`
	}
	refuseArm := ""
	if refuse != "" {
		refuseArm = fmt.Sprintf("if grep -q ' %s ' \"$manifest\"; then echo 'refused %s' >&2; exit 1; fi", refuse, refuse)
	}
	writeExec(t, filepath.Join(dir, referenceLang, "driver"), fmt.Sprintf(`#!/bin/sh
manifest="$1"
surface="$2"
out="$3"
[ "$surface" = list ] && exit 0
case "$surface" in
wire|message|json-text) ;;
*) exit 2 ;;
esac
%s
grep -E '^(instance|message|json-text) ' "$manifest" | while read -r kind name a b c d; do
	case "$kind" in
	instance) src=$c; dest="$out/$name" ;;
	message) src=$d; dest="$out/$name" ;;
	json-text) src=$c; dest="$out/$name.json" ;;
	esac
	cp "$src" "$dest"
	%s
done
exit 0
`, refuseArm, tail))
	return dir
}

// portWrites lays out what `harness run` leaves for a leg: <surface>/<case>.
func portWrites(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "writes")
	for rel, text := range files {
		writeFile(t, filepath.Join(dir, rel), text)
	}
	return dir
}

func (c fakeCorpus) crossRead(t *testing.T, reference, writes string) (string, bool, []coverageRow) {
	t.Helper()
	var out bytes.Buffer
	rows, ok, err := crossRead(&out, c.m, c.reports, reference, filepath.Join(t.TempDir(), "work"), "zz", writes, nil)
	if err != nil {
		t.Fatalf("cross-read: %v\n%s", err, out.String())
	}
	return out.String(), ok, rows
}

// faithful is a port that wrote exactly the corpus: the wire golden on both
// wire surfaces and the committed text on json-write.
func (c fakeCorpus) faithful(t *testing.T) map[string]string {
	t.Helper()
	wire, err := os.ReadFile(c.wire)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(c.json)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"wire/alpha":            string(wire),
		"json-read/alpha":       string(wire),
		"json-write/alpha.json": string(text),
	}
}

func TestCrossReadAcceptsAFaithfulPort(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok, rows := c.crossRead(t, fakeReference(t, "", false), portWrites(t, c.faithful(t)))
	if !ok {
		t.Fatalf("a port whose bytes are C++'s and round-trip was refused:\n%s", out)
	}
	mustSay(t, out, []string{"C++ accepts every case zz answered"}, []string{"FAILURES"})
	for _, s := range []string{"wire", "json-read", "json-write"} {
		if r, found := findRow(rows, "zz", s); !found || r.Answered != 1 || r.Cases != 1 || r.Direction != directionCppReads {
			t.Errorf("%s: the row does not count the case: %+v", s, r)
		}
	}
	if r, found := findRow(rows, "zz", "message"); !found || !r.SurfaceAbsent {
		t.Errorf("message: a surface the port left no writes for is not named absent: %+v", r)
	}
}

// TestCrossReadPortBytesNotCpps: the port's bytes round-trip through C++ and
// are still not C++'s own canonical write — red, by case.
func TestCrossReadPortBytesNotCpps(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	files := c.faithful(t)
	files["wire/alpha"] = "\x01\x02\x03\x05"
	out, ok, rows := c.crossRead(t, fakeReference(t, "", false), portWrites(t, files))
	if ok {
		t.Fatalf("a port's bytes that are not C++'s left the cross-read green:\n%s", out)
	}
	mustSay(t, out, []string{"zz / wire: the port's bytes are not C++'s canonical write: alpha: first difference at byte 3"}, nil)
	if r, _ := findRow(rows, "zz", "wire"); r.Failed != 1 {
		t.Errorf("the failing case is not counted: %+v", r)
	}
	if r, _ := findRow(rows, "zz", "json-read"); r.Answered != 1 || r.Failed != 0 {
		t.Errorf("the failure leaked to another surface: %+v", r)
	}
}

// TestCrossReadReferenceRefuses: C++ will not load the port's bytes, and the
// refusal is named by case, from the one-case rerun.
func TestCrossReadReferenceRefuses(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok, _ := c.crossRead(t, fakeReference(t, "alpha", false), portWrites(t, c.faithful(t)))
	if ok {
		t.Fatalf("C++ refusing the port's bytes left the cross-read green:\n%s", out)
	}
	mustSay(t, out, []string{"zz / wire: alpha: C++ REFUSED the port's bytes (the reference exited 1)", "refused alpha"}, nil)
}

// TestCrossReadReferenceResavesDifferently: C++ accepts the bytes and writes
// something else back.
func TestCrossReadReferenceResavesDifferently(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok, _ := c.crossRead(t, fakeReference(t, "", true), portWrites(t, c.faithful(t)))
	if ok {
		t.Fatalf("a re-save that differs left the cross-read green:\n%s", out)
	}
	mustSay(t, out, []string{"zz / json-write: C++ re-saved the port's bytes differently: alpha.json", "zz / wire: C++ re-saved"}, nil)
}

// TestCrossReadSkipsAbsentCases: what the port answered ABSENT is the first
// direction's to count; the second re-reads only what was written.
func TestCrossReadSkipsAbsentCases(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	out, ok, rows := c.crossRead(t, fakeReference(t, "alpha", false), portWrites(t, map[string]string{"wire/alpha.absent": ""}))
	if !ok {
		t.Fatalf("an absent case was re-read:\n%s", out)
	}
	if r, found := findRow(rows, "zz", "wire"); !found || r.Cases != 0 || r.SurfaceAbsent {
		t.Errorf("the absent case is counted: %+v", r)
	}
}
