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
			scoreAnswer("go", "wire", out, expectation{name, want}, "", r)
		}
		if err := os.WriteFile(filepath.Join(out, "blob_shared.absent"), []byte("blob_shared"), 0o644); err != nil {
			t.Fatal(err)
		}
		scoreAnswer("go", "wire", out, want[0], m.Instances[0].GoWireRefusal, r)
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
		scoreAnswer("go", "wire", out, expectation{"blob_shared", []byte("golden wire")}, "blob_shared", r)
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
		scoreAnswer("go", "wire", out, expectation{"blob_shared", []byte("golden wire")}, "blob_shared", r)
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
		scoreAnswer("go", "wire", out, expectation{"blob_shared", []byte("golden wire")}, "blob_shared", r)
		if len(r.failures) != 1 || r.missing != 0 {
			t.Fatalf("wire scores = absent %d, failures %v; wrong refusal name must be red", r.missing, r.failures)
		}
	})

	t.Run("other legs keep the base bytes expectation", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		want := []byte("golden wire")
		if err := os.WriteFile(filepath.Join(out, "blob_shared"), want, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("cpp", "wire", out, expectation{"blob_shared", want}, "blob_shared", r)
		if r.pass != 1 || len(r.failures) != 0 {
			t.Fatalf("cpp wire scores = pass %d, failures %v; want base byte pass", r.pass, r.failures)
		}
	})
}
