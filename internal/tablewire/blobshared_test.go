// A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): the engine's writer
// refuses a graph whose two slots name one blob, by the name `blob_shared`, and
// its tolerant reader keeps the one node and counts the extra naming in the
// report, exactly as generated Go does (§17.1, §17.5).
package tablewire_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func blobCatalogModel(t *testing.T) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/blobs"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func blobCatalogField(t *testing.T, inst *tabletext.Instance, key string) *tabletext.Field {
	t.Helper()
	f, ok := inst.FieldByKey(key)
	if !ok {
		t.Fatalf("Catalog has no field %q", key)
	}
	return f
}

func TestEngineWireBlobShared(t *testing.T) {
	t.Parallel()
	m := blobCatalogModel(t)
	catalog := m.Lookup("Catalog")

	// THE TOLERANT READ keeps the value, resolves both slots to the ONE decoded
	// node, and counts the extra naming (§2.5). This is the value generated
	// Go's tolerant Load of the same fixture gives: name "twin", no head, no
	// note, and thumb and alias one four-byte blob.
	wire, err := os.ReadFile("../../testdata/wire/tables/blob_shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	loaded := m.New(catalog)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, loaded, wire, &report)
	if err != nil {
		t.Fatalf("Decode refused the shared-blob wire: %v", err)
	}
	if !ok {
		t.Fatal("Decode stopped on the shared-blob wire")
	}
	if report.BlobShared != 1 {
		t.Fatalf("BlobShared = %d, want 1 (report %+v)", report.BlobShared, report)
	}
	if report.Malformed || report.Unknown != 0 || report.KindMismatch != 0 || report.Widened != 0 || report.Clamped != 0 || report.Duplicate != 0 {
		t.Fatalf("the shared-blob read moved another event: %+v", report)
	}
	if name := blobCatalogField(t, loaded, "name"); string(name.Cell.Str) != "twin" {
		t.Fatalf("name = %q, want %q", name.Cell.Str, "twin")
	}
	if head := blobCatalogField(t, loaded, "head"); head.Cell.Node != nil {
		t.Fatal("head is not null")
	}
	if note := blobCatalogField(t, loaded, "note"); note.Cell.Blob != nil {
		t.Fatal("note is not null")
	}
	thumb := blobCatalogField(t, loaded, "thumb")
	alias := blobCatalogField(t, loaded, "alias")
	want := []byte{0x10, 0x20, 0x30, 0x40}
	if thumb.Cell.Blob == nil || !bytes.Equal(thumb.Cell.Blob.Data, want) {
		t.Fatalf("thumb = %v, want % x", thumb.Cell.Blob, want)
	}
	if alias.Cell.Blob != thumb.Cell.Blob {
		t.Fatal("thumb and alias do not name the one decoded blob node")
	}

	// THE WRITER refuses that value by name and writes nothing (§2.5, §17.1).
	refused, err := tablewire.Encode(m, loaded)
	if len(refused) != 0 {
		t.Fatalf("Encode wrote %d bytes for a blob two slots name", len(refused))
	}
	if err == nil || !strings.Contains(err.Error(), "blob_shared") || !strings.Contains(err.Error(), "§2.5") {
		t.Fatalf("Encode of a shared blob = %v, want the named refusal blob_shared", err)
	}

	// AND A FRESH value built from the model refuses the same way, so the
	// answer does not depend on the read that produced the first one.
	fresh := m.New(catalog)
	blob := &tabletext.Blob{Data: append([]byte(nil), want...)}
	blobCatalogField(t, fresh, "thumb").Cell.Blob = blob
	blobCatalogField(t, fresh, "alias").Cell.Blob = blob
	refused, err = tablewire.Encode(m, fresh)
	if len(refused) != 0 {
		t.Fatalf("Encode wrote %d bytes for a fresh blob two slots name", len(refused))
	}
	if err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("Encode of a fresh shared blob = %v, want the named refusal blob_shared", err)
	}

	// A value with TWO blobs is legal, so the refusal is the sharing and not
	// the blob field itself.
	twice := m.New(catalog)
	blobCatalogField(t, twice, "thumb").Cell.Blob = &tabletext.Blob{Data: append([]byte(nil), want...)}
	blobCatalogField(t, twice, "alias").Cell.Blob = &tabletext.Blob{Data: append([]byte(nil), want...)}
	if wire, err := tablewire.Encode(m, twice); err != nil || len(wire) == 0 {
		t.Fatalf("Encode of two distinct blobs = %d bytes, %v", len(wire), err)
	}
}
