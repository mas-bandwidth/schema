package gotable

import (
	"testing"
)

// TestTableWireOverATextTransport is the card's gate (table-wire-over-text-
// transports): a table has two text carriers (docs/SPEC-TABLES.md §16.8) —
// the base64 of the binary wire (SaveText/LoadText) and the JSON text form
// (FromJson/ToJson) — and EVERY table has both, fixed and variable alike.
// Each carrier round-trips byte for byte against the binary wire (the value a
// carrier reads back saves to the very bytes the original saved to), and each
// reader refuses damaged or newer input by name.
func TestTableWireOverATextTransport(t *testing.T) {
	t.Parallel()
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, textTransportFixedSchema, textTransportFixedProbe)
	})
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, textTransportVariableSchema, textTransportVariableProbe)
	})
}

const textTransportFixedSchema = `package probe
fixed table Root {
	id uint32
	name string(8)
}
`

const textTransportFixedProbe = `package probe
import("bytes";"testing";"encoding/base64")
func TestTableWireOverATextTransport(t *testing.T) {
	var v Root
	v.Id = 7
	copy(v.Name[:], "hi")
	v.NameLength = 2
	wire := make([]byte, RootMeasure(&v))
	if n := RootSave(&v, wire); n != int64(len(wire)) { t.Fatalf("wire save: %d", n) }

	// THE BASE64 CARRIER IS THE WIRE: SaveText is base64 of Save, and what
	// LoadText reads back saves to the same bytes.
	text, err := RootSaveText(&v)
	if err != nil { t.Fatalf("save text: %v", err) }
	if text != base64.StdEncoding.EncodeToString(wire) {
		t.Fatalf("save text is not base64 of the wire")
	}
	got, err := RootLoadText(text, nil)
	if err != nil || got == nil || got.Id != 7 || string(got.Name[:got.NameLength]) != "hi" {
		t.Fatalf("base64 round trip: %+v %v", got, err)
	}
	again := make([]byte, RootMeasure(got))
	if RootSave(got, again) != int64(len(again)) || !bytes.Equal(again, wire) {
		t.Fatalf("base64 carrier is not byte for byte: %x vs %x", again, wire)
	}

	// THE JSON CARRIER round-trips to the same wire bytes.
	jsonText := make([]byte, RootToJsonMeasure(&v))
	if RootToJson(&v, jsonText) != int64(len(jsonText)) { t.Fatal("json write") }
	var j Root
	if !RootFromJson(&j, jsonText, nil) || j.Id != 7 || string(j.Name[:j.NameLength]) != "hi" {
		t.Fatalf("json round trip: %+v", j)
	}
	again = make([]byte, RootMeasure(&j))
	if RootSave(&j, again) != int64(len(again)) || !bytes.Equal(again, wire) {
		t.Fatalf("json carrier is not byte for byte: %x vs %x", again, wire)
	}

	// A CORRUPTED BASE64 BODY REFUSES BY NAME.
	_, err = RootLoadText("!!!not-base64!!!", nil)
	if err == nil || err.Error() != "base64_damaged" {
		t.Fatalf("corrupt base64: %v", err)
	}

	// A TRUNCATED DECODED WIRE REFUSES BY NAME.
	_, err = RootLoadText(base64.StdEncoding.EncodeToString(wire[:4]), nil)
	if err == nil || err.Error() != "wire_damaged" {
		t.Fatalf("truncated wire: %v", err)
	}

	// A NEWER FORM BYTE REFUSES BY NAME.
	for _, form := range []byte{2, 9} {
		newer := append([]byte(nil), wire...)
		newer[0] = form
		_, err = RootLoadText(base64.StdEncoding.EncodeToString(newer), nil)
		if err == nil || err.Error() != "unknown_form" {
			t.Fatalf("form %d: %v", form, err)
		}
	}

	// A DAMAGED JSON DOCUMENT REFUSES BY NAME.
	var r TableReport
	if RootFromJson(&v, []byte("{ \"id\": 7 not json"), &r) || !r.Malformed {
		t.Fatalf("damaged json: %+v", r)
	}
}
`

// A VARIABLE table with a pointer, so the base64 carrier crosses a node table
// and not only a root record.
const textTransportVariableSchema = `package probe
table Node {
	id uint32
	name string(8)
	next *Node
}
`

const textTransportVariableProbe = `package probe
import("bytes";"testing";"encoding/base64")
func TestTableWireOverATextTransport(t *testing.T) {
	var b NodeBuilder
	if !b.Init() { t.Fatal("init") }
	defer b.Shutdown()
	var r TableReport
	if !NodeFromJson(&b, []byte("{\"id\":7,\"name\":\"hi\",\"next\":{\"id\":8,\"name\":\"yo\"}}"), &r) || !b.Lock() {
		t.Fatalf("source: %+v", r)
	}
	v := b.AsConst()
	wire := make([]byte, NodeMeasure(v))
	if n := NodeSave(v, wire); n != int64(len(wire)) { t.Fatalf("wire save: %d", n) }

	// THE BASE64 CARRIER IS THE WIRE: SaveText is base64 of Save, and what
	// LoadText reads back saves to the same bytes.
	text, err := NodeSaveText(v)
	if err != nil { t.Fatalf("save text: %v", err) }
	if text != base64.StdEncoding.EncodeToString(wire) {
		t.Fatalf("save text is not base64 of the wire")
	}
	got, err := NodeLoadText(text, nil)
	if err != nil || got == nil || got.Id != 7 || string(got.Name[:got.NameLength]) != "hi" {
		t.Fatalf("base64 round trip: %+v %v", got, err)
	}
	again := make([]byte, NodeMeasure(got))
	if NodeSave(got, again) != int64(len(again)) || !bytes.Equal(again, wire) {
		t.Fatalf("base64 carrier is not byte for byte: %x vs %x", again, wire)
	}

	// THE JSON CARRIER round-trips to the same wire bytes.
	jsonText := make([]byte, NodeToJsonMeasure(v))
	if NodeToJson(v, jsonText) != int64(len(jsonText)) { t.Fatal("json write") }
	var jb NodeBuilder
	if !jb.Init() { t.Fatal("init") }
	defer jb.Shutdown()
	if !NodeFromJson(&jb, jsonText, nil) || !jb.Lock() { t.Fatal("json read") }
	again = make([]byte, NodeMeasure(jb.AsConst()))
	if NodeSave(jb.AsConst(), again) != int64(len(again)) || !bytes.Equal(again, wire) {
		t.Fatalf("json carrier is not byte for byte: %x vs %x", again, wire)
	}

	// A CORRUPTED BASE64 BODY REFUSES BY NAME.
	_, err = NodeLoadText("!!!not-base64!!!", nil)
	if err == nil || err.Error() != "base64_damaged" {
		t.Fatalf("corrupt base64: %v", err)
	}

	// A TRUNCATED DECODED WIRE REFUSES BY NAME.
	_, err = NodeLoadText(base64.StdEncoding.EncodeToString(wire[:4]), nil)
	if err == nil || err.Error() != "wire_damaged" {
		t.Fatalf("truncated wire: %v", err)
	}

	// A NEWER FORM BYTE REFUSES BY NAME.
	for _, form := range []byte{2, 9} {
		newer := append([]byte(nil), wire...)
		newer[0] = form
		_, err = NodeLoadText(base64.StdEncoding.EncodeToString(newer), nil)
		if err == nil || err.Error() != "unknown_form" {
			t.Fatalf("form %d: %v", form, err)
		}
	}

	// A DAMAGED JSON DOCUMENT REFUSES BY NAME.
	var db NodeBuilder
	if !db.Init() { t.Fatal("init") }
	defer db.Shutdown()
	var dr TableReport
	if NodeFromJson(&db, []byte("{ \"id\": 7 not json"), &dr) || !dr.Malformed {
		t.Fatalf("damaged json: %+v", dr)
	}
}
`
