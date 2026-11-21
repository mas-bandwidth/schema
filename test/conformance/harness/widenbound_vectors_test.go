package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WIDENED FIXED BOUND ROWS (docs/SPEC-TABLES.md §21.1, "a grown bound").
//
// A fixed array `[N]T` written by a build whose bound was smaller reads as
// the writer's elements followed by ONE DEFAULT ELEMENT PER NEW SLOT: an
// empty union, a record at its declared defaults, a null pointer, a zero
// scalar. The rows below are that read for every leg: each wire is written
// by the engine from a corpus unit whose named bounds are NARROWED, and the
// corpus unit itself, unchanged, is the reader. No unit is added, so every
// leg already builds the reader.
//
// The wires are fixtures: this test rebuilds each from the engine and holds
// the committed bytes to it. `WIDENBOUND_WRITE=1 go test -run
// TestWidenBoundVectorsPinned ./test/conformance/harness` rewrites them, and
// `make conformance-generate` then writes the answers.

// widenBoundNarrow is one fixed array whose bound the writer declared
// smaller: a field of a table or type, or an arm of a union.
type widenBoundNarrow struct {
	owner, field string
	bound        int64
}

type widenBoundRow struct {
	unit, root string
	narrow     []widenBoundNarrow
	doc        string // the writer's value, as its text
	// A variable root: the wire is the writer's file. A fixed root: the wire
	// is the writer's message batch, announce its announcement, and file the
	// READER's file of the value the batch lands (the message row's pair).
	wire, announce, file string
}

const widenBoundWireDir = "testdata/conformance/tables/wire"

var widenBoundTransaction = []widenBoundNarrow{
	{"Transaction", "pending", 1},
	{"Transaction", "checkpoints", 1},
	{"Transaction", "snapshots", 1},
	{"ToolBody", "spans", 1},
	{"ToolMessage", "marks", 1},
}

const widenBoundTransactionDoc = `"body":{"transact":{"reason":"grow","pending":[{"tally":5}],"checkpoints":[11],"snapshots":[{"start":{"line":1,"column":2},"stop":{"line":3,"column":4}}]}}`

var widenBoundRows = []widenBoundRow{
	{ // an array of unions, an optional array of scalars and one of records, inside a union arm
		unit: "messagedemo", root: "ToolEnvelope", narrow: widenBoundTransaction,
		doc:  `{"sequence":7,` + widenBoundTransactionDoc + `}`,
		wire: "widenbound_envelope.bin",
	},
	{ // an array of scalars that IS a union arm
		unit: "messagedemo", root: "ToolEnvelope", narrow: widenBoundTransaction,
		doc:  `{"sequence":8,"body":{"spans":[-9]}}`,
		wire: "widenbound_envelope_spans.bin",
	},
	{ // an array of pointers
		unit: "streamdemo", root: "Feed", narrow: []widenBoundNarrow{{"Feed", "pair", 1}},
		doc:  `{"id":3,"pair":[{"&node":1,"data":"AQ==","next":null,"links":[]}]}`,
		wire: "widenbound_feed.bin",
	},
	{ // arrays of scalars and of records whose defaults are not zero
		unit: "tblvarevo1", root: "EvoDoc", narrow: []widenBoundNarrow{{"EvoDoc", "scores", 1}, {"EvoDoc", "items", 1}, {"EvoDoc", "fixed_sub", 1}},
		doc:  `{"scores":[-7],"items":[{"count":10}],"fixed_sub":[{"count":20}]}`,
		wire: "widenbound_evodoc.bin",
	},
	{ // the message form at a fixed root: the same arrays, and an optional array of enums
		unit: "messagedemo", root: "ToolMessage", narrow: widenBoundTransaction,
		doc:      `{"sequence":9,` + widenBoundTransactionDoc + `,"marks":["Write"]}`,
		wire:     "widenbound_msg_peer.bin",
		announce: "widenbound_conn.bin",
		file:     "widenbound_msg.bin",
	},
}

// widenBoundWriter is a fresh load of a corpus unit with the named bounds
// narrowed: the older build that wrote the row.
func widenBoundWriter(m *Manifest, row widenBoundRow) (*ir.Unit, error) {
	unit, err := newUnits(m).get(row.unit)
	if err != nil {
		return nil, err
	}
	for _, n := range row.narrow {
		var f *ir.Field
		owners := []*ir.Struct{unit.Tables[n.owner], unit.Structs[n.owner]}
		for _, st := range owners {
			if st == nil {
				continue
			}
			for _, sf := range st.Fields {
				if sf.Name == n.field {
					f = sf
				}
			}
		}
		for _, un := range []*ir.Union{unit.TableUnions[n.owner], unit.Unions[n.owner]} {
			if un == nil {
				continue
			}
			for _, v := range un.Variants {
				if v.Name == n.field {
					f = v.F
				}
			}
		}
		if f == nil || f.Array != ir.ArrayFixed || f.ArrayBound <= n.bound {
			return nil, fmt.Errorf("%s: %s.%s is not a fixed array wider than %d", row.unit, n.owner, n.field, n.bound)
		}
		f.ArrayBound = n.bound
	}
	return unit, nil
}

// widenBoundBytes is every file one row owes, by name.
func widenBoundBytes(m *Manifest, row widenBoundRow) (map[string][]byte, error) {
	older, err := widenBoundWriter(m, row)
	if err != nil {
		return nil, err
	}
	writer := tabletext.NewModel(older)
	value := writer.New(writer.Lookup(row.root))
	var rep tabletext.Report
	if !writer.Read(value, []byte(row.doc), &rep) || rep.Refused {
		return nil, fmt.Errorf("%s: the writer's value does not read: %+v", row.wire, rep)
	}
	newer, err := newUnits(m).get(row.unit)
	if err != nil {
		return nil, err
	}
	reader := tabletext.NewModel(newer)
	landed := reader.New(reader.Lookup(row.root))
	out := map[string][]byte{}
	if row.announce == "" {
		if out[row.wire], err = tablewire.EncodeVariable(writer, value); err != nil {
			return nil, err
		}
		if ok, err := tablewire.DecodeVariable(reader, landed, out[row.wire], &rep); !ok || err != nil || rep.Refused {
			return nil, fmt.Errorf("%s: the reader refuses the narrower writer: %v %+v", row.wire, err, rep)
		}
	} else {
		if out[row.wire], err = tablewire.EncodeMessages(writer, []*tabletext.Instance{value}); err != nil {
			return nil, err
		}
		out[row.announce] = tablewire.Announce(older)
		var vocabulary tablewire.Vocabulary
		if err := vocabulary.AnnounceRead(out[row.announce], &rep); err != nil {
			return nil, err
		}
		// the peer's build version is the manifest's connection column
		if c, err := m.LookupConnection("widenbound_conn"); err == nil && c.BuildVersion != vocabulary.BuildVersion() {
			return nil, fmt.Errorf("%s: the narrower peer announces build version 0x%016x, the manifest states 0x%016x", row.announce, vocabulary.BuildVersion(), c.BuildVersion)
		} else if err != nil {
			return nil, fmt.Errorf("%s: the narrower peer announces build version 0x%016x: %w", row.announce, vocabulary.BuildVersion(), err)
		}
		if n, ok, err := tablewire.DecodeMessages(reader, []*tabletext.Instance{landed}, out[row.wire], &vocabulary, &rep); n != 1 || !ok || err != nil || rep.Refused {
			return nil, fmt.Errorf("%s: the reader refuses the narrower peer: %v %+v", row.wire, err, rep)
		}
		if out[row.file], err = tablewire.Encode(reader, landed); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func TestWidenBoundVectorsPinned(t *testing.T) {
	root := conformanceRoot(t)
	t.Chdir(root)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range widenBoundRows {
		files, err := widenBoundBytes(m, row)
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range files {
			path := filepath.Join(widenBoundWireDir, name)
			if os.Getenv("WIDENBOUND_WRITE") == "1" {
				if err := os.WriteFile(path, want, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the engine's bytes for the narrower writer (WIDENBOUND_WRITE=1 rewrites it)", path)
			}
		}
	}
}
