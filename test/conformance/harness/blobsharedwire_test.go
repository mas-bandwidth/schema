package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireBlobSharedIsANamedRefusal(t *testing.T) {
	t.Parallel()

	t.Run("named refusal keeps the other wire rows passing", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		wire := filepath.Join(out, "blob_shared.bin")
		if err := os.WriteFile(wire, []byte("golden wire"), 0o644); err != nil {
			t.Fatal(err)
		}
		manifest := filepath.Join(out, "MANIFEST.txt")
		text := "unit blobdemo testdata/conformance/tables/blobs.schema\n" +
			"instance blob_shared blobdemo Catalog " + wire + " no-text go-refusal=blob_shared\n"
		if err := os.WriteFile(manifest, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := ReadManifest(manifest, filepath.Join(out, "json"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := expectations(m, "wire", nil, "")
		if err != nil || len(want) != 1 || m.Instances[0].GoWireRefusal != "blob_shared" {
			t.Fatalf("manifest wire refusal = %+v, err %v", m.Instances, err)
		}
		r := &result{total: 96}
		for i := range 95 {
			name := fmt.Sprintf("ordinary_%02d", i)
			want := []byte(name + " bytes")
			if err := os.WriteFile(filepath.Join(out, name), want, 0o644); err != nil {
				t.Fatal(err)
			}
			scoreAnswer("go", "wire", out, expectation{name: name, want: want}, Instance{}, r)
		}
		if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte("blob_shared"), 0o644); err != nil {
			t.Fatal(err)
		}
		scoreAnswer("go", "wire", out, want[0], m.Instances[0], r)
		if r.pass != 95 || r.missing != 1 || len(r.failures) != 0 {
			t.Fatalf("wire scores = pass %d, absent %d, failures %v; want 95, 1, none", r.pass, r.missing, r.failures)
		}
		results := map[string]map[string]*result{"go": {}}
		for _, surface := range surfaces {
			results["go"][surface] = &result{}
		}
		results["go"]["wire"] = r
		var table bytes.Buffer
		if !report(&table, []string{"go"}, results) {
			t.Fatalf("named refusal made the wire surface red: %s", table.String())
		}
		if !strings.Contains(table.String(), "pass 95/95 +1a") {
			t.Fatalf("wire cell does not show one named absence: %s", table.String())
		}
	})

	t.Run("bytes do not answer the refusal", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared"), []byte("golden wire"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("go", "wire", out, expectation{name: "blob_shared", want: []byte("golden wire")}, Instance{GoWireRefusal: "blob_shared"}, r)
		if len(r.failures) != 1 || r.missing != 0 {
			t.Fatalf("wire scores = absent %d, failures %v; bytes must be red", r.missing, r.failures)
		}
	})

	t.Run("an unnamed absence does not answer the refusal", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("go", "wire", out, expectation{name: "blob_shared", want: []byte("golden wire")}, Instance{GoWireRefusal: "blob_shared"}, r)
		if len(r.failures) != 1 || r.missing != 0 {
			t.Fatalf("wire scores = absent %d, failures %v; unnamed absence must be red", r.missing, r.failures)
		}
	})

	t.Run("a different named absence does not answer the refusal", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte("table_refused"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("go", "wire", out, expectation{name: "blob_shared", want: []byte("golden wire")}, Instance{GoWireRefusal: "blob_shared"}, r)
		if len(r.failures) != 1 || r.missing != 0 {
			t.Fatalf("wire scores = absent %d, failures %v; wrong refusal name must be red", r.missing, r.failures)
		}
	})

	t.Run("another leg may answer the named refusal", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte("blob_shared"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("cpp", "wire", out, expectation{name: "blob_shared", want: []byte("golden wire")}, Instance{GoWireRefusal: "blob_shared"}, r)
		if r.missing != 1 || r.named != 1 || len(r.failures) != 0 {
			t.Fatalf("cpp wire scores = absent %d, named %d, failures %v; want one named refusal", r.missing, r.named, r.failures)
		}
	})

	t.Run("another leg's wrong refusal name is an ordinary absence", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte("table_refused"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("cpp", "wire", out, expectation{name: "blob_shared", want: []byte("golden wire")}, Instance{GoWireRefusal: "blob_shared"}, r)
		if r.missing != 1 || r.named != 0 {
			t.Fatalf("cpp wire scores = absent %d, named %d; a wrong name must not count as the refusal", r.missing, r.named)
		}
	})

	t.Run("other legs keep the base bytes expectation", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		want := []byte("golden wire")
		if err := os.WriteFile(filepath.Join(out, "blob_shared"), want, 0o644); err != nil {
			t.Fatal(err)
		}
		// scoreAnswer asks the instance which refusal the leg owes: the
		// reference is not listed, so it owes the bytes
		inst := Instance{GoWireRefusal: "blob_shared"}
		r := &result{total: 1}
		scoreAnswer("cpp", "wire", out, expectation{name: "blob_shared", want: want}, inst, r)
		if r.pass != 1 || len(r.failures) != 0 {
			t.Fatalf("cpp wire scores = pass %d, failures %v; want base byte pass", r.pass, r.failures)
		}
	})
}

// TestWireRefusedByNamesThePortsBesidesGo pins the refused-by=<ports> marker:
// the listed ports owe the same named refusal Go owes, every other port is
// still held to the committed bytes, and the marker refuses to stand alone,
// to name go, or to name an empty port (docs/SPEC-TABLES.md §2.5).
func TestWireRefusedByNamesThePortsBesidesGo(t *testing.T) {
	t.Parallel()
	read := func(markers string) (*Manifest, error) {
		out := t.TempDir()
		manifest := filepath.Join(out, "MANIFEST.txt")
		text := "unit blobdemo testdata/conformance/tables/blobs.schema\n" +
			"instance blob_shared blobdemo Catalog blob_shared.bin no-text " + markers + "\n"
		if err := os.WriteFile(manifest, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return ReadManifest(manifest, filepath.Join(out, "json"))
	}
	m, err := read("go-refusal=blob_shared refused-by=js,dart")
	if err != nil {
		t.Fatal(err)
	}
	inst := m.Instances[0]
	for lang, want := range map[string]string{"go": "blob_shared", "js": "blob_shared", "dart": "blob_shared", "cpp": "", "cs": ""} {
		if got := inst.WireRefusal(lang); got != want {
			t.Errorf("WireRefusal(%q) = %q, want %q", lang, got, want)
		}
	}
	for _, bad := range []string{"refused-by=js", "go-refusal=blob_shared refused-by=go", "go-refusal=blob_shared refused-by=js,", "go-refusal=blob_shared refused-by=js refused-by=cs"} {
		if _, err := read(bad); err == nil {
			t.Errorf("markers %q: the manifest read accepted them", bad)
		}
	}
	// a listed port answering the named absence passes; bytes from it are red
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte("blob_shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &result{total: 1}
	scoreAnswer("js", "wire", out, expectation{name: "blob_shared", want: []byte("golden wire")}, inst, r)
	if r.missing != 1 || len(r.failures) != 0 {
		t.Fatalf("js named refusal scores = absent %d, failures %v; want 1, none", r.missing, r.failures)
	}
	bytesOut := t.TempDir()
	if err := os.WriteFile(filepath.Join(bytesOut, "blob_shared"), []byte("golden wire"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = &result{total: 1}
	scoreAnswer("js", "wire", bytesOut, expectation{name: "blob_shared", want: []byte("golden wire")}, inst, r)
	if len(r.failures) != 1 {
		t.Fatalf("js bytes for a refused instance scored %v; must be red", r.failures)
	}
	// an unlisted port is held to the bytes
	r = &result{total: 1}
	scoreAnswer("cpp", "wire", bytesOut, expectation{name: "blob_shared", want: []byte("golden wire")}, inst, r)
	if r.pass != 1 || len(r.failures) != 0 {
		t.Fatalf("cpp bytes scored pass %d failures %v; the reference still writes them", r.pass, r.failures)
	}
}
