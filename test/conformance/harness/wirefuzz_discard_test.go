package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The legacy pipe has several refusal flags, but the contract has one verdict.
// Every diagnostic and undefined payload is ignored after refusal. An accepted
// result must agree in every canonical byte, independent of all counters.
func TestWireVerdictComparesOnlyAcceptanceAndAcceptedValues(t *testing.T) {
	refused := oracleAnswer{report: Counts{Refused: true}}
	accepted := oracleAnswer{accepted: true, encoded: []byte{1, 0, 7}}
	diagnostics := Counts{Unknown: 8, KindMismatch: 3, Widened: 2, Clamped: 5, Duplicate: 1}
	for _, root := range []*wireRoot{{}, {variable: true}, {builder: true}, {retain: true}, {message: true}} {
		for _, reply := range []legReply{
			{loaded: false},
			{loaded: true, report: Counts{Refused: true}},
			{loaded: true, report: Counts{Malformed: true}},
			{loaded: true, report: Counts{Refused: true, Malformed: true}},
			{loaded: false, report: diagnostics, measure: 999999, saved: []byte{8}, retained: 17, retainLost: 2},
		} {
			if v := wireVerdict(root, reply, refused, 2); v != "" {
				t.Fatal(v)
			}
			if v := wireVerdict(root, reply, accepted, 2); v == "" {
				t.Fatal("refused accepted value passed")
			}
		}
		good := legReply{loaded: true, report: diagnostics, measure: -1, saved: accepted.encoded, retained: 999, retainLost: 23}
		if v := wireVerdict(root, good, accepted, 2); v != "" {
			t.Fatal(v)
		}
		if v := wireVerdict(root, good, refused, 2); v == "" {
			t.Fatal("repair counters hid an invalid acceptance")
		}
		good.saved = []byte{1, 0, 8}
		if v := wireVerdict(root, good, accepted, 2); v == "" {
			t.Fatal("changed accepted value passed")
		}
		good.saved, good.saveFail = nil, true
		if v := wireVerdict(root, good, accepted, 2); v == "" {
			t.Fatal("accepted value with no write-back passed")
		}
	}
}

func TestWireFuzzBuilderRetainOracle(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "tblrt1", "Node", false, true)
	if err != nil {
		t.Fatal(err)
	}
	root.builder = true
	wire := wireBytes(t, "testdata/wire/tables/retain_rt2.bin")
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	want := wireBytes(t, "testdata/wire/tables/retain_rt1_save.bin")
	if !answer.accepted || !bytes.Equal(answer.encoded, want) {
		t.Fatalf("retaining read did not preserve the pinned complete value: accepted=%t got=%x want=%x", answer.accepted, answer.encoded, want)
	}
}

func fuzzFixtureRoot(t *testing.T, schema string, message bool) *wireRoot {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{Units: []Unit{{Key: "probe", Paths: []string{path}}}}
	root, err := newWireRoot(newUnits(m), "probe", "Root", message, false)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWireFuzzMessageComparesEveryBody(t *testing.T) {
	root := fuzzFixtureRoot(t, "package probe\nfixed table Root { n int32 }\n", true)
	batch := []*tabletext.Instance{root.model.New(root.def), root.model.New(root.def)}
	for i, input := range []string{`{"n":7}`, `{"n":41}`} {
		if !root.model.Read(batch[i], []byte(input), &tabletext.Report{}) {
			t.Fatal("fixture JSON")
		}
	}
	wire, err := tablewire.EncodeMessages(root.model, batch)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.accepted || !bytes.Equal(answer.encoded, wire) {
		t.Fatal("oracle lost part of an accepted message batch")
	}
	if !root.model.Read(batch[1], []byte(`{"n":42}`), &tabletext.Report{}) {
		t.Fatal("second body mutation")
	}
	changed, err := tablewire.EncodeMessages(root.model, batch)
	if err != nil {
		t.Fatal(err)
	}
	if v := wireVerdict(root, legReply{loaded: true, saved: changed}, answer, len(wire)); v == "" {
		t.Fatal("second-body value mutation passed")
	}
	short, err := tablewire.EncodeMessage(root.model, batch[0])
	if err != nil {
		t.Fatal(err)
	}
	if v := wireVerdict(root, legReply{loaded: true, saved: short}, answer, len(wire)); v == "" {
		t.Fatal("first-body-only write-back passed")
	}
	truncated := wire[:len(wire)-1]
	refused, err := root.oracle(truncated)
	if err != nil {
		t.Fatal(err)
	}
	if refused.accepted || len(refused.encoded) != 0 {
		t.Fatal("damaged batch produced a value")
	}
}

// An unknown timestamp is legal evolution; unknown malformed bools and kind
// mismatches are refusals. The independent framing keeps the valid control from
// passing merely because a historical fixed root rejects every form-1 fixture.
func TestWireFuzzEvolutionAndDamage(t *testing.T) {
	root := fuzzFixtureRoot(t, "package probe\ntable Root { n int32 }\n", false)
	file := func(kind byte, payload []byte, id string) []byte {
		out := append([]byte{1, 1, kind}, payload...)
		out = append(out, 0)
		out = binary.LittleEndian.AppendUint64(out, ir.TableWireId(id))
		return binary.LittleEndian.AppendUint64(out, 1)
	}
	for _, tc := range []struct {
		name     string
		wire     []byte
		accepted bool
	}{
		{"unknown timestamp", file(36, []byte{7, 0, 0, 0, 0, 0, 0, 0}, "future"), true},
		{"known int32", file(4, []byte{7, 0, 0, 0}, "n"), true},
		{"timestamp mismatch", file(36, []byte{7, 0, 0, 0, 0, 0, 0, 0}, "n"), false},
		{"unknown bool valid", file(1, []byte{1}, "future"), true},
		{"unknown bool invalid", file(1, []byte{2}, "future"), false},
		{"unknown bool truncated", file(1, nil, "future"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer, err := root.oracle(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			if answer.accepted != tc.accepted {
				t.Fatalf("accepted=%t want=%t", answer.accepted, tc.accepted)
			}
			if !tc.accepted {
				if len(answer.encoded) != 0 {
					t.Fatal("refusal saved defaults")
				}
				return
			}
			want := root.model.New(root.def)
			if tc.name == "known int32" && !root.model.Read(want, []byte(`{"n":7}`), &tabletext.Report{}) {
				t.Fatal("expected value")
			}
			encoded, err := tablewire.Encode(root.model, want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(answer.encoded, encoded) {
				t.Fatalf("accepted value differs: got %x want %x", answer.encoded, encoded)
			}
		})
	}
}
