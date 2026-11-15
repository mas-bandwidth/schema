package gotable

import "testing"

func TestGoJSONBytesReadCanonicalBase64Only(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, `package probe
table Root {
	data bytes(4)
	blob *bytes
}
`, `package probe
import ("bytes"; "testing")

func TestJSONBytesCanonicalBase64(t *testing.T) {
	read := func(text string) (*RootBuilder, TableReport, bool) {
		var b RootBuilder
		if !b.Init() { panic("init") }
		var report TableReport
		ok := RootFromJSON(&b, []byte(text), &report)
		return &b, report, ok
	}

	// Both byte fields accept the canonical padded spelling, and the writer
	// returns that same spelling on a JSON round trip.
	b, report, ok := read("{\"data\":\"QUI=\",\"blob\":\"QUI=\"}")
	if !ok || report != (TableReport{}) { t.Fatalf("canonical input: ok=%v report=%+v", ok, report) }
	if !b.Lock() { t.Fatal("canonical builder lock") }
	root := b.AsConst()
	text := make([]byte, RootToJSONMeasure(root))
	if RootToJSON(root, text) != int64(len(text)) { t.Fatal("canonical JSON write") }
	if bytes.Count(text, []byte("\"QUI=\"")) != 2 { t.Fatalf("writer did not emit canonical base64 for both fields: %s", text) }
	b.Shutdown()
	b2, report2, ok2 := read(string(text))
	if !ok2 || report2 != (TableReport{}) { t.Fatalf("canonical round trip: %s ok=%v report=%+v", text, ok2, report2) }
	if !b2.Lock() { t.Fatal("round-trip builder lock") }
	root2 := b2.AsConst()
	text2 := make([]byte, RootToJSONMeasure(root2))
	if RootToJSON(root2, text2) != int64(len(text2)) || !bytes.Equal(text, text2) { t.Fatalf("ToJSON round trip changed text: %s -> %s", text, text2) }
	b2.Shutdown()

	// The empty string is the canonical encoding of the empty byte sequence.
	b3, emptyReport, emptyOK := read("{\"data\":\"\",\"blob\":\"\"}")
	if !emptyOK || emptyReport != (TableReport{}) { t.Fatalf("empty input: ok=%v report=%+v", emptyOK, emptyReport) }
	b3.Shutdown()

	for _, value := range []string{"QUI", "QUJ=", "QQ=", "Q=UI"} {
		for _, field := range []string{"data", "blob"} {
			text := "{\"" + field + "\":\"" + value + "\"}"
			bad, badReport, badOK := read(text)
			bad.Shutdown()
			if badOK || !badReport.Refused() { t.Errorf("accepted non-canonical %s value %q: report=%+v", field, value, badReport) }
		}
	}
}
`)
}
