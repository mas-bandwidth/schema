package gotable

import (
	"testing"
)

// TestTableWireOverATextTransport is the card's gate (table-wire-over-text-
// transports): a table has two text carriers (docs/SPEC-TABLES.md §16.8) —
// the base64 of the binary wire (SaveText/LoadText) and the JSON text form
// (FromJSON/ToJSON) — and EVERY table has both, fixed and variable alike.
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
	// A RECOVERABLE DAMAGE IS STILL DAMAGE. The tolerant load can return true
	// and still report malformed (TestBuilderCountRecovery's shape: a declared
	// list count past its payload); LoadText is the strict read (§16.8, §6.5)
	// and refuses whenever the report holds an error, whatever the load said.
	t.Run("variable_recovered_malformed", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, textTransportRecoverSchema, textTransportRecoverProbe)
	})
	t.Run("fixed_recovered_clamped", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, textTransportClampSchema, textTransportClampProbe)
	})
}

// The fixture TestBuilderCountRecovery uses (lists_test.go), carried as text.
const textTransportRecoverSchema = `package probe
table Child { n int32 = 7
 values []int32 }
table Root { values []int32
 next int32
 children []Child
 child *Child }
`

const textTransportRecoverProbe = `package probe
import("errors";"testing";"encoding/base64")
func file(count uint64) []byte {var ids TableIds;b:=make([]byte,256);w:=TableWriter{Buffer:b,Ids:&ids};w.Put8(1);w.ID(RootTableFields[0].ID);w.Put8(14);w.PutLeb(uint64(1+tableLebBytes(count)+4));w.Put8(4);w.PutLeb(count);w.Put32(19);w.ID(RootTableFields[1].ID);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer();return b[:w.Offset]}
func TestTableWireOverATextTransport(t *testing.T) {
	wire := file(1 << 30)
	// The fixture is damaged: the tolerant builder load discards the whole
	// load (docs/SPEC-TABLES.md §4), answering false and reporting malformed.
	var b RootBuilder
	if !b.Init() { t.Fatal("init") }
	defer b.Shutdown()
	var tolerant TableReport
	if RootLoadBuilderTolerant(&b, wire, &tolerant) || !tolerant.Malformed {
		t.Fatalf("fixture is not damaged: %+v", tolerant)
	}
	text := base64.StdEncoding.EncodeToString(wire)
	got, err := RootLoadText(text, nil)
	if got != nil || err == nil || err.Error() != "malformed" || !errors.Is(err, TableRefuseMalformed) {
		t.Fatalf("nil report: value %v, error %v; want nil and malformed", got != nil, err)
	}
	var r TableReport
	got, err = RootLoadText(text, &r)
	if got != nil || err == nil || err.Error() != "malformed" || !r.Malformed {
		t.Fatalf("supplied report: value %v, error %v, report %+v; want nil, malformed, Malformed", got != nil, err, r)
	}
	// A supplied report receives THIS read's report: earlier counts in it
	// neither leak into the verdict nor survive the call.
	stale := TableReport{Malformed: true, Unknown: 3}
	if v, err := RootLoadText(base64.StdEncoding.EncodeToString(file(1)), &stale); v == nil || err != nil || stale != (TableReport{}) {
		t.Fatalf("clean read with a stale report: value %v, error %v, report %+v", v != nil, err, stale)
	}
}
`

// A fixed table read from a wider writer's wire: the tolerant Load clamps the
// string and answers true; LoadText refuses it as clamped, by name.
const textTransportClampSchema = `package probe
fixed table Wide {
	id uint32
	name string(16)
}
fixed table Root {
	id uint32
	name string(8)
}
`

const textTransportClampProbe = `package probe
import("errors";"testing")
func TestTableWireOverATextTransport(t *testing.T) {
	var w Wide
	w.ID = 7
	copy(w.Name[:], "0123456789ab")
	w.NameLength = 12
	text, err := WideSaveText(&w)
	if err != nil { t.Fatalf("wide save text: %v", err) }
	wire, _ := WideLoadText(text, nil)
	if wire == nil { t.Fatal("wide round trip") }
	buf := make([]byte, WideMeasure(&w))
	WideSave(&w, buf)
	var v Root
	var tolerant TableReport
	if !RootLoadTolerant(&v, buf, &tolerant) || tolerant.Clamped == 0 {
		t.Fatalf("fixture is not the recoverable shape: %+v", tolerant)
	}
	got, err := RootLoadText(text, nil)
	if got != nil || err == nil || err.Error() != "clamped" || !errors.Is(err, TableRefuseClamped) {
		t.Fatalf("nil report: value %v, error %v; want nil and clamped", got != nil, err)
	}
	var r TableReport
	got, err = RootLoadText(text, &r)
	if got != nil || err == nil || err.Error() != "clamped" || r.Clamped == 0 {
		t.Fatalf("supplied report: value %v, error %v, report %+v; want nil, clamped, Clamped", got != nil, err, r)
	}
}
`

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
	v.ID = 7
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
	if err != nil || got == nil || got.ID != 7 || string(got.Name[:got.NameLength]) != "hi" {
		t.Fatalf("base64 round trip: %+v %v", got, err)
	}
	again := make([]byte, RootMeasure(got))
	if RootSave(got, again) != int64(len(again)) || !bytes.Equal(again, wire) {
		t.Fatalf("base64 carrier is not byte for byte: %x vs %x", again, wire)
	}

	// THE JSON CARRIER round-trips to the same wire bytes.
	jsonText := make([]byte, RootToJSONMeasure(&v))
	if RootToJSON(&v, jsonText) != int64(len(jsonText)) { t.Fatal("json write") }
	var j Root
	if !RootFromJSONTolerant(&j, jsonText, nil) || j.ID != 7 || string(j.Name[:j.NameLength]) != "hi" {
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
	if RootFromJSONTolerant(&v, []byte("{ \"id\": 7 not json"), &r) || !r.Malformed {
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
	if !NodeFromJSONTolerant(&b, []byte("{\"id\":7,\"name\":\"hi\",\"next\":{\"id\":8,\"name\":\"yo\"}}"), &r) || !b.Lock() {
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
	if err != nil || got == nil || got.ID != 7 || string(got.Name[:got.NameLength]) != "hi" {
		t.Fatalf("base64 round trip: %+v %v", got, err)
	}
	again := make([]byte, NodeMeasure(got))
	if NodeSave(got, again) != int64(len(again)) || !bytes.Equal(again, wire) {
		t.Fatalf("base64 carrier is not byte for byte: %x vs %x", again, wire)
	}

	// THE JSON CARRIER round-trips to the same wire bytes.
	jsonText := make([]byte, NodeToJSONMeasure(v))
	if NodeToJSON(v, jsonText) != int64(len(jsonText)) { t.Fatal("json write") }
	var jb NodeBuilder
	if !jb.Init() { t.Fatal("init") }
	defer jb.Shutdown()
	if !NodeFromJSONTolerant(&jb, jsonText, nil) || !jb.Lock() { t.Fatal("json read") }
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
	if NodeFromJSONTolerant(&db, []byte("{ \"id\": 7 not json"), &dr) || !dr.Malformed {
		t.Fatalf("damaged json: %+v", dr)
	}
}
`
