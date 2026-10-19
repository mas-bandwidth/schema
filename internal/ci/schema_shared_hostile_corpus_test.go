package ci

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The committed hostile-input corpus (schema-shared-hostile-corpus).
//
// One corpus of adversarial inputs for schema, shared by every port. Each
// input's expected verdict is taken from the GO ENGINE (internal/tablewire,
// internal/tabletext) at the time the cardinal was built, never from a
// generated reader. The drift check below regenerates each verdict from the
// engine and fails when the committed manifest differs, so the shared corpus
// cannot silently drift from what the engine actually does.
//
// A case is a raw carrier payload plus the committed expectation:
//
//   - carrier "wire": the raw table-wire bytes handed to tablewire.Decode
//   - carrier "text": the raw table-text bytes handed to tabletext.Model.Read
//
// The verdict is a pair: accept (true/false) and, for a refusal, the reason.
// A named refusal is the engine's own message; a damaged read (ok=false with
// no engine error) is the reason "damaged", which is the report carrying a
// malformed/kind-mismatch event.
//
// Provenance of every case (where it was gathered from):
//   - valid-wire / text-valid: base accept cases (hand-built)
//   - empty / truncated-tail: bounds, one byte below a minimal wire
//   - byteff: a form byte at 0xff, beyond the highest form the engine carries
//   - form+1: a form byte one above the current form (newer-form refusal)
//   - byte02: a single hostile byte that is neither a form nor a valid message
type hostileVerdict struct {
	accept bool
	reason string
}

type hostileInput struct {
	name     string
	table    string
	carrier  string
	input    []byte
	proven   string
	expected hostileVerdict
}

var hostileCorpus = []hostileInput{
	{
		name: "valid-wire", table: "ShipEntry", carrier: "wire",
		input:    []byte{0x01, 0x01, 0x0c, 0x06, 0x41, 0x75, 0x72, 0x6f, 0x72, 0x61, 0x02, 0x0e, 0x0a, 0x04, 0x02, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x5d, 0x5a, 0xbd, 0x66, 0xcd, 0xd7, 0x21, 0x2d, 0x73, 0x2b, 0xa8, 0x9c, 0xe0, 0xcc, 0xd0, 0x95, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		proven:   "hand-built accept case from the engine's own encode/decode",
		expected: hostileVerdict{accept: true},
	},
	{
		name: "empty", table: "ShipEntry", carrier: "wire",
		input:    nil,
		proven:   "bounds: zero bytes is below the smallest readable wire",
		expected: hostileVerdict{accept: false, reason: "damaged"},
	},
	{
		name: "truncated-tail", table: "ShipEntry", carrier: "wire",
		input:    []byte{0x01, 0x01, 0x0c, 0x06, 0x41, 0x75, 0x72, 0x6f, 0x72, 0x61, 0x02, 0x0e, 0x0a, 0x04, 0x02, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x5d, 0x5a, 0xbd, 0x66, 0xcd, 0xd7, 0x21, 0x2d, 0x73, 0x2b, 0xa8, 0x9c, 0xe0, 0xcc, 0xd0, 0x95},
		proven:   "bounds: the wire body cut short of its declared length",
		expected: hostileVerdict{accept: false, reason: "damaged"},
	},
	{
		name: "byteff", table: "ShipEntry", carrier: "wire",
		input:    []byte{0xff},
		proven:   "bounds: a form byte past the highest form the engine carries",
		expected: hostileVerdict{accept: false, reason: "wire form 255 is newer than form 1, the one this reader carries — refused, and no damage is reported (docs/SPEC-TABLES.md §3)"},
	},
	{
		name: "form+1", table: "ShipEntry", carrier: "wire",
		input:    []byte{0x02},
		proven:   "edge: a form byte one above the current form (newer-form refusal)",
		expected: hostileVerdict{accept: false, reason: "the message was refused: message_form_as_file (docs/SPEC-TABLES.md §3.3)"},
	},
	{
		name: "byte02", table: "ShipEntry", carrier: "wire",
		input:    []byte{0x02},
		proven:   "hostile single byte that is neither a form nor a valid message",
		expected: hostileVerdict{accept: false, reason: "the message was refused: message_form_as_file (docs/SPEC-TABLES.md §3.3)"},
	},
	{
		name: "text-valid", table: "ShipEntry", carrier: "text",
		input:    []byte(`{ "name": "Aurora", "hardpoints": [1, 2] }`),
		proven:   "text-carrier base accept case",
		expected: hostileVerdict{accept: true},
	},
}

func hostileModel(t *testing.T) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{repoRoot(t) + "/tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// engineVerdict regenerates the engine's verdict for one corpus input, exactly
// the way the committed manifest was produced. This is the oracle: it reads
// the Go engine (internal/tablewire, internal/tabletext) and never a
// generated reader.
func engineVerdict(t *testing.T, m *tabletext.Model, in hostileInput) hostileVerdict {
	t.Helper()
	switch in.carrier {
	case "wire":
		inst := m.New(m.Lookup(in.table))
		var r tabletext.Report
		ok, err := tablewire.Decode(m, inst, in.input, &r)
		if err != nil && tablewire.Refused(err) {
			return hostileVerdict{accept: false, reason: err.Error()}
		}
		if err != nil {
			return hostileVerdict{accept: false, reason: "engine-error: " + err.Error()}
		}
		if !ok {
			return hostileVerdict{accept: false, reason: "damaged"}
		}
		return hostileVerdict{accept: true}
	case "text":
		inst := m.New(m.Lookup(in.table))
		var r tabletext.Report
		if !m.Read(inst, in.input, &r) {
			return hostileVerdict{accept: false, reason: "damaged"}
		}
		return hostileVerdict{accept: true}
	default:
		t.Fatalf("unknown carrier %q", in.carrier)
		return hostileVerdict{}
	}
}

// TestSchemaSharedHostileCorpus is the drift gate. It regenerates every verdict
// from the engine and fails when the committed manifest differs.
func TestSchemaSharedHostileCorpus(t *testing.T) {
	t.Parallel()
	m := hostileModel(t)
	for _, in := range hostileCorpus {
		in := in
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			got := engineVerdict(t, m, in)
			if got != in.expected {
				t.Fatalf("drift: %s (%s carrier) committed verdict %+v but the engine now says %+v (provenance: %s)",
					in.name, in.carrier, in.expected, got, in.proven)
			}
		})
	}
}

// TestSchemaSharedHostileCorpusNegativeControl is the planted-bad-pattern
// control. It takes one committed expectation, deliberately flips it (planting
// a bad pattern), and asserts the drift comparison is RED against the flipped
// expectation. This proves the gate has a blade: with the plant in, the check
// fails; with it out, TestSchemaSharedHostileCorpus is green. A plant direction
// that stops being caught is the drift gate going blind.
func TestSchemaSharedHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	m := hostileModel(t)
	for i := range hostileCorpus {
		in := hostileCorpus[i]
		var planted hostileVerdict
		if in.expected.accept {
			planted = hostileVerdict{accept: false, reason: "damaged"}
		} else {
			planted = hostileVerdict{accept: true}
		}
		got := engineVerdict(t, m, in)
		if got == planted {
			t.Fatalf("negative control: %s (%s carrier): the planted bad pattern %+v is NOT caught — the drift gate is blind",
				in.name, in.carrier, planted)
		}
	}
}
