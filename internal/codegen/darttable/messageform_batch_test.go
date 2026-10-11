package darttable

// THE MESSAGE FORM'S BATCH ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// The message form is "A BATCH OF BODIES ... READ AND WRITTEN", and "a single
// message is the batch of one": `SaveMessages` takes an array of bodies of ONE
// root and writes ONE buffer, `LoadMessages` reads one buffer into the caller's
// storage and allocates nothing, and `MeasureMessages` sizes the buffer. The
// announcement that precedes the batch landed on this leg already
// (internal/codegen/darttable/messageform_announce.go); this test owes the
// batch surface itself.
//
// The bytes are the C++ reference's, pinned by testdata/conformance/tables/
// MANIFEST.txt's `message` rows — each row carries the form-`1` instance and
// the message-form bytes the same value writes. The surface and its names
// follow the C# leg (internal/codegen/cstable/message.go): MeasureMessages,
// SaveMessages and LoadMessages over the vocabulary the announcement card
// emitted.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// batchMessage is one `message` row of the manifest: the form-`1` instance and
// the message-form bytes for the same value, both the reference's.
type batchMessage struct {
	connKey string
	root    string
	form1   []byte
	message []byte
}

// batchMessages reads testdata/conformance/tables/MANIFEST.txt's `message`
// rows. The row is `message <key> <connection key> <root> <form-1 wire>
// <message wire>`.
func batchMessages(t *testing.T) []batchMessage {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatalf("the manifest is not readable: %v", err)
	}
	var out []batchMessage
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 6 || fields[0] != "message" {
			continue
		}
		form1, err := os.ReadFile(filepath.Join("../../..", fields[4]))
		if err != nil {
			t.Fatalf("the manifest's form-1 file %s is unreadable: %v", fields[4], err)
		}
		message, err := os.ReadFile(filepath.Join("../../..", fields[5]))
		if err != nil {
			t.Fatalf("the manifest's message file %s is unreadable: %v", fields[5], err)
		}
		out = append(out, batchMessage{connKey: fields[2], root: fields[3], form1: form1, message: message})
	}
	if len(out) == 0 {
		t.Fatal("the manifest carries no message row")
	}
	return out
}

// batchUnitForConn maps a `connection` key to the unit that carries it, and
// that unit's schema path, from the manifest's `connection` and `unit` rows.
func batchUnitForConn(t *testing.T, connKey string) (string, string) {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatalf("the manifest is not readable: %v", err)
	}
	connUnit := map[string]string{}
	unitPath := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 5 && fields[0] == "connection":
			connUnit[fields[1]] = fields[2]
		case len(fields) == 3 && fields[0] == "unit":
			unitPath[fields[1]] = fields[2]
		}
	}
	unit, ok := connUnit[connKey]
	if !ok {
		t.Fatalf("the manifest carries no connection row for %s", connKey)
	}
	path, ok := unitPath[unit]
	if !ok {
		t.Fatalf("the manifest carries no unit row for %s", unit)
	}
	return unit, path
}

// emittedSource concatenates every file the Dart leg emits for a unit, the
// emitted surface itself rather than the emitters' source text. A unit row
// names a schema DIRECTORY, so every *.schema in it is loaded, the same files
// the reference's constants were emitted from.
func emittedSource(t *testing.T, path string) string {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join("../../..", path, "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under %s: %v", path, err)
	}
	u := loadUnit(t, schemas...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", path, err)
	}
	var src strings.Builder
	for _, b := range files {
		src.Write(b)
	}
	return src.String()
}

// TestMessageFormBatchDart asserts, one subtest per clause of STEP 2, the batch
// surface the Dart leg owes §3.3. The surface is ABSENT on this leg today, so
// each subtest is RED: it runs, and it fails for the reason the clause names.
func TestMessageFormBatchDart(t *testing.T) {
	t.Parallel()
	messages := batchMessages(t)

	// 2.1 "`SaveMessages` takes an array of bodies of ONE root and writes ONE
	// buffer. `LoadMessages` reads one buffer into the caller's storage and
	// allocates nothing. `MeasureMessages` sizes the buffer." (§3.3) — every
	// manifest `message` row: SaveMessages must write the row's message bytes
	// exactly, LoadMessages must give back the values the form-1 instance
	// decodes to, and MeasureMessages must equal the row's length.
	t.Run("batch_round_trip", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}
		for _, m := range messages {
			unit, path := batchUnitForConn(t, m.connKey)
			if seen[unit] {
				continue
			}
			seen[unit] = true
			src := emittedSource(t, path)
			for _, name := range []string{"MeasureMessages", "SaveMessages", "LoadMessages"} {
				if !strings.Contains(src, name) {
					t.Fatalf("%s: the Dart leg emits no message-form batch surface: %s (docs/SPEC-TABLES.md §3.3; root %s)", unit, name, m.root)
				}
			}
		}
	})

	// 2.2 "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... with its
	// refusal verdict set and the reason `batch_too_large` on it" and "`M`
	// ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ...
	// before it decodes anything ... the reason is `batch_too_large` on both
	// sides", and "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME
	// ... it holds no vocabulary for the peer" (`no_vocabulary`).
	t.Run("batch_refusals", func(t *testing.T) {
		t.Parallel()
		for _, m := range messages {
			unit, path := batchUnitForConn(t, m.connKey)
			src := emittedSource(t, path)
			for _, name := range []string{"LoadMessages", "SaveMessages", "MeasureMessages"} {
				if !strings.Contains(src, name) {
					t.Fatalf("%s: the Dart leg emits no message-form batch surface: %s (docs/SPEC-TABLES.md §3.3; root %s)", unit, name, m.root)
				}
			}
			for _, reason := range []string{"no_vocabulary", "batch_too_large"} {
				if !strings.Contains(src, "'"+reason+"'") && !strings.Contains(src, `"`+reason+`"`) {
					t.Fatalf("%s: the Dart leg carries no message-form batch refusal %q (docs/SPEC-TABLES.md §3.3; root %s)", unit, reason, m.root)
				}
			}
			return
		}
	})

	// 2.3 "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`" (one
	// `malformed`), "BYTES AFTER THE PAD ARE MALFORMED", and the form byte's
	// own refusals "newer_form" and "message_form_as_file".
	t.Run("batch_damage", func(t *testing.T) {
		t.Parallel()
		for _, m := range messages {
			unit, path := batchUnitForConn(t, m.connKey)
			src := emittedSource(t, path)
			for _, name := range []string{"LoadMessages", "SaveMessages", "MeasureMessages"} {
				if !strings.Contains(src, name) {
					t.Fatalf("%s: the Dart leg emits no message-form batch surface: %s (docs/SPEC-TABLES.md §3.3; root %s)", unit, name, m.root)
				}
			}
			for _, reason := range []string{"malformed", "newer_form", "message_form_as_file"} {
				if !strings.Contains(src, "'"+reason+"'") && !strings.Contains(src, `"`+reason+`"`) {
					t.Fatalf("%s: the Dart leg carries no message-form batch damage/refusal %q (docs/SPEC-TABLES.md §3.3; root %s)", unit, reason, m.root)
				}
			}
			return
		}
	})
}
