package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireBlobSharedRefusalIsAnAnsweredCase(t *testing.T) {
	t.Parallel()
	for _, lang := range []string{"go", "cpp", "js", "unported"} {
		for _, diagnostic := range []string{"", "blob_shared", "different local diagnostic"} {
			t.Run(lang+"/"+diagnostic, func(t *testing.T) {
				out := t.TempDir()
				if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte(diagnostic), 0600); err != nil {
					t.Fatal(err)
				}
				r := &result{total: 1}
				scoreAnswer(lang, "wire", out, expectation{name: "blob_shared", want: []byte("old bytes")}, Instance{WireRefused: true}, r)
				if r.pass != 1 || r.missing != 0 || r.named != 0 || len(r.failures) != 0 {
					t.Fatalf("refusal did not score PASS: %+v", r)
				}
			})
		}
		for _, written := range []bool{false, true} {
			out := t.TempDir()
			if written {
				if err := os.WriteFile(filepath.Join(out, "blob_shared"), []byte("old bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r := &result{total: 1}
			scoreAnswer(lang, "wire", out, expectation{name: "blob_shared", want: []byte("old bytes")}, Instance{WireRefused: true}, r)
			if len(r.failures) != 1 || r.pass != 0 {
				t.Fatalf("missing refusal or invalid bytes passed: %+v", r)
			}
		}
	}
	out := t.TempDir()
	r := &result{total: 96}
	for i := range 95 {
		name := fmt.Sprintf("ordinary_%02d", i)
		want := []byte(name)
		if err := os.WriteFile(filepath.Join(out, name), want, 0600); err != nil {
			t.Fatal(err)
		}
		scoreAnswer("go", "wire", out, expectation{name: name, want: want}, Instance{}, r)
	}
	if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	scoreAnswer("go", "wire", out, expectation{name: "blob_shared"}, Instance{WireRefused: true}, r)
	results := map[string]map[string]*result{"go": {}}
	for _, surface := range surfaces {
		results["go"][surface] = &result{}
	}
	results["go"]["wire"] = r
	var table bytes.Buffer
	if !report(&table, []string{"go"}, results) || !strings.Contains(table.String(), "pass 96/96") {
		t.Fatalf("semantic refusal missing from completed cases: %s", table.String())
	}
}

// The README lets a driver spell the wire-refused row `refused` (the shape of
// every read surface) or the historical `<case>.absent`. Both score PASS; a
// `refused` without its newline, or any value bytes, is a failure. The Swift
// spec port's gap 4 (2026-11-22): the harness judged `refused` as bytes, so a
// port that followed the README could never score the row.
func TestWireRefusedSpelledRefusedScoresPass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body string
		pass bool
	}{
		{"refused\n", true},
		{"refused", false},
		{"refused\nextra", false},
		{"old bytes", false},
	} {
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared"), []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("swift", "wire", out, expectation{name: "blob_shared", want: []byte("old bytes")}, Instance{WireRefused: true}, r)
		if got := r.pass == 1 && len(r.failures) == 0; got != tc.pass {
			t.Errorf("%q: pass=%v, want %v (%+v)", tc.body, got, tc.pass, r)
		}
	}
}

// The absence marker is the ANSWER's name plus `.absent`: on json-write the
// answer is `<case>.json`, so the marker is `<case>.json.absent`, and a bare
// `<case>.absent` is a driver that wrote nothing (the Swift spec port's gap
// 12, 2026-11-22; test/conformance/README.md).
func TestJsonWriteAbsentMarkerIsTheAnswerNamePlusAbsent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		marker  string
		missing bool
	}{
		{"cfg.json.absent", true},
		{"cfg.absent", false},
	} {
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, tc.marker), nil, 0600); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("swift", "json-write", out, expectation{name: "cfg.json", want: []byte("{}\n")}, Instance{}, r)
		if got := r.missing == 1 && len(r.failures) == 0; got != tc.missing {
			t.Errorf("%s: counted absent=%v, want %v (%+v)", tc.marker, got, tc.missing, r)
		}
		if !tc.missing && len(r.failures) != 1 {
			t.Errorf("%s: want one failure, got %+v", tc.marker, r)
		}
	}
}

// Legacy marker syntax remains readable, but cannot weaken any port's verdict.
func TestWireRefusalMarkersApplyToEveryPort(t *testing.T) {
	t.Parallel()
	read := func(markers string) (*Manifest, error) {
		dir := t.TempDir()
		path := filepath.Join(dir, "MANIFEST.txt")
		text := "unit blobdemo testdata/conformance/tables/blobs.schema\ninstance blob_shared blobdemo Catalog blob_shared.bin no-text " + markers + "\n"
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		return ReadManifest(path, filepath.Join(dir, "json"))
	}
	m, err := read("go-refusal=blob_shared refused-by=js,dart")
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"go", "js", "dart", "cpp", "cs", "unported"} {
		if !m.Instances[0].WireRefused {
			t.Fatalf("%s inherited an invalid acceptance exemption", lang)
		}
	}
	for _, bad := range []string{"refused-by=js", "go-refusal=blob_shared refused-by=go", "go-refusal=blob_shared refused-by=js,", "go-refusal=blob_shared refused-by=js refused-by=cs"} {
		if _, err := read(bad); err == nil {
			t.Errorf("invalid markers accepted: %q", bad)
		}
	}
}
