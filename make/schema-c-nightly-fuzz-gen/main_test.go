package main

import "testing"

func TestScanClassifiesFuzzableAndTypeOnlyHeaders(t *testing.T) {
	t.Parallel()
	const tableHeader = `static SCHEMA_UNUSED int guarded_load( Guarded * value, const uint8_t * buffer, int64_t bytes, TableReport * report )
static SCHEMA_UNUSED int guarded_from_json( Guarded * value, const char * text, int64_t bytes, TableReport * report )`
	got := scan(tableHeader)
	if len(got) != 1 || got[0].name != "guarded" || got[0].loadTyp != "Guarded" || got[0].jsonTyp != "Guarded" {
		t.Fatalf("scan(table header) = %#v", got)
	}

	const typeOnlyHeader = `typedef struct ArmorConfig { int32_t rating; } ArmorConfig;`
	if got := scan(typeOnlyHeader); len(got) != 0 {
		t.Fatalf("scan(type-only header) = %#v", got)
	}
	if readerAPI.MatchString(typeOnlyHeader) {
		t.Fatal("type-only header was classified as an unrecognised reader")
	}

	const unknownReader = `static SCHEMA_UNUSED int guarded_load(Guarded *value, const uint8_t *wire, long bytes, TableReport *report)`
	if got := scan(unknownReader); len(got) != 0 {
		t.Fatalf("unknown signature unexpectedly matched: %#v", got)
	}
	if !readerAPI.MatchString(unknownReader) {
		t.Fatal("unrecognised reader signature would be silently skipped")
	}
}
