// A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): the engine's writer
// refuses a graph whose two slots name one blob, by the name `blob_shared`, and
// its reader refuses a wire that names one blob record from two slots (§6.5).
package tablewire_test

import (
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

	// EVERY READ REFUSES IT (§2.5, §6.5): one blob record named from two
	// slots is a condition the wire alone shows, so the read hands back no
	// value and names blob_shared.
	wire, err := os.ReadFile("../../testdata/wire/tables/blob_shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	loaded := m.New(catalog)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, loaded, wire, &report)
	if ok || tablewire.RefusalReason(err) != tablewire.ReasonBlobShared || !report.Refused {
		t.Fatalf("Decode of the shared-blob wire = %v, %v, %+v; want the blob_shared refusal", ok, err, report)
	}
	if thumb := blobCatalogField(t, loaded, "thumb"); thumb.Cell.Blob != nil {
		t.Fatal("a refused read left a value standing")
	}

	// THE WRITER refuses a shared blob by name and writes nothing (§2.5, §17.1).
	want := []byte{0x10, 0x20, 0x30, 0x40}
	fresh := m.New(catalog)
	blob := &tabletext.Blob{Data: append([]byte(nil), want...)}
	blobCatalogField(t, fresh, "thumb").Cell.Blob = blob
	blobCatalogField(t, fresh, "alias").Cell.Blob = blob
	refused, err := tablewire.Encode(m, fresh)
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
