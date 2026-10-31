package gotable

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE MESSAGE FORM's NEGATIVE CONTROLS (docs/SPEC-TABLES.md §3.3). Each gate of
// the Go message form gets a reversed witness: one fault planted in a COPY of
// the generated source (no tracked file is written), and the gate's own probe
// required to go red on its own message. A control that patches nothing is
// itself red, so a renamed emitter line cannot leave a control vacuous.

// sabotageGenerated applies each replacement to every generated Go file and
// fails the test when none of them matched anything. The pairs carry the
// gofmt'd spelling; the generated source is gofmt'd before it is returned.
func sabotageGenerated(t *testing.T, files map[string][]byte, what string, pairs ...[2]string) {
	t.Helper()
	patched := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		s := string(data)
		for _, pair := range pairs {
			if next := strings.ReplaceAll(s, pair[0], pair[1]); next != s {
				patched++
				s = next
			}
		}
		files[name] = []byte(s)
	}
	if patched == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage patched nothing", what)
	}
}

// requireRed holds a control's verdict: the run went red, and on the line the
// gate owes that fault rather than on a compile error or another clause.
func requireRed(t *testing.T, what string, out []byte, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s left the gate GREEN\n%s", what, out)
	}
	if !bytes.Contains(out, []byte(want)) {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s went red, but not on %q\n%s", what, want, out)
	}
}

// messageGoldenRow builds TestGoMessageFormGoldens' probe for one manifest
// message row, exactly as the gate builds it.
func messageGoldenRow(t *testing.T, name string) (string, func(func(map[string][]byte)) ([]byte, error)) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units, connections, rows := parseManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	for _, row := range rows {
		if row.name != name {
			continue
		}
		conn := connections[row.connection]
		u := loadUnitPaths(t, root, units[conn.unit])
		source := messageProbe(u.Package, u, row.root, filepath.Join(root, conn.path), filepath.Join(root, row.file), filepath.Join(root, row.batch))
		return row.root, func(edit func(map[string][]byte)) ([]byte, error) {
			return runGoGoldensEdited(t, u, source, edit)
		}
	}
	t.Fatalf("the manifest carries no message row %q", name)
	return "", nil
}

// TestGoMessageFormGoldensNegativeControl: TestGoMessageFormGoldens over the
// login_full row, once with the WRITER's batch count off by one (the saved
// batch must differ from the reference's bytes) and once with the READER's
// (the reference batch must not load as one body).
func TestGoMessageFormGoldensNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated goldens)")
	t.Run("write", func(t *testing.T) {
		t.Parallel()
		_, run := messageGoldenRow(t, "login_full")
		out, err := run(func(files map[string][]byte) {
			sabotageGenerated(t, files, "batch count write", [2]string{"buffer[1] = byte(len(values) - 1)", "buffer[1] = byte(len(values))"})
		})
		requireRed(t, "a batch count written one high", out, err, "the batch bytes differ from the reference")
	})
	t.Run("read", func(t *testing.T) {
		t.Parallel()
		root, run := messageGoldenRow(t, "login_full")
		out, err := run(func(files map[string][]byte) {
			sabotageGenerated(t, files, "batch count read", [2]string{"return r, int64(data[1]) + 1", "return r, int64(data[1]) + 2"})
		})
		requireRed(t, "a batch count read one high", out, err, root+"LoadMessages: count=")
	})
}

// TestGoMessageFormRefusalsNegativeControl: three of TestGoMessageFormRefusals'
// clauses, each with the refusal it asserts planted out of the generated
// reader or writer.
func TestGoMessageFormRefusalsNegativeControl(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
	a int32
	b bool
	c uint8
}
`
	controls := []struct {
		name, probe, want string
		pairs             [][2]string
	}{
		{"no_vocabulary", msgNoVocabularyProbe, "--- FAIL: TestNoVocabulary",
			[][2]string{{"if v == nil || !v.Announced {", "if v == nil {"}}},
		{"batch_too_large", msgBatchTooLargeProbe, "measure over 256 bodies",
			[][2]string{{"if len(values) < 1 || len(values) > TableMessageBatchMax {", "if len(values) < 1 {"}}},
		{"form_byte", msgFormByteProbe, "form 1: count=",
			[][2]string{{"if data[0] != 2 {\n\t\ttableMessageRefuse(&r.Report, \"newer_form\")", "if false {\n\t\ttableMessageRefuse(&r.Report, \"newer_form\")"}}},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := runGeneratedEdited(t, schema, "package probe\n"+c.probe, func(files map[string][]byte) {
				sabotageGenerated(t, files, c.name, c.pairs...)
			})
			requireRed(t, "the "+c.name+" refusal planted out", out, err, c.want)
		})
	}
}

// TestGoMessageFormStrictNegativeControl: TestGoMessageFormStrict's
// strict_other_version clause with the build version comparison planted out
// of the announcement reader.
func TestGoMessageFormStrictNegativeControl(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
	a int32
	b bool
	c uint8
}
`
	out, err := runGeneratedEdited(t, schema, "package probe\n"+msgStrictOtherProbe, func(files map[string][]byte) {
		sabotageGenerated(t, files, "strict build version", [2]string{"if v.Strict && v.BuildVersion != BuildVersion {", "if false {"})
	})
	requireRed(t, "the strict comparison planted out", out, err, "the strict read accepted an endpoint of another build version")
}

// TestGoMessageFormVersioningNegativeControl: TestGoMessageFormVersioning's
// newer_writer_unknown_field clause with the reader's unknown counter planted
// out (the field is still stepped over, so only the count can turn it red).
func TestGoMessageFormVersioningNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runMessagePairEdited(t, newerWriter, newerReader, "\tw.A = 11\n\tw.B = -5\n\tw.Extra = 99", `
	if !ok {
		t.Fatalf("a newer writer's batch was refused: %+v", report)
	}
	if report.Unknown != 1 {
		t.Fatalf("the newer field is not counted once as unknown: %+v", report)
	}
`, func(files map[string][]byte) {
		sabotageGenerated(t, files, "unknown counter", [2]string{"r.Report.Unknown++\n\t\t\tif !tableMessageSkip(", "if !tableMessageSkip("})
	})
	requireRed(t, "the message reader's unknown counter planted out", out, err, "the newer field is not counted once as unknown")
}

// TestGoMessageFormFixedOnlyNegativeControl: TestGoMessageFormFixedOnly's check
// over the real unit's table source with a message verb planted on the plain
// root and one taken off the fixed root; both must be named.
func TestGoMessageFormFixedOnlyNegativeControl(t *testing.T) {
	t.Parallel()
	files := generate(t, `package probe

table Loose { n int32 }
fixed table Tight { n int32 }
`)
	var table string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") || strings.HasSuffix(name, "TableMessage.go") {
			table += string(data)
		}
	}
	if len(fixedOnlyViolations(table)) != 0 {
		t.Fatalf("the clean unit is not clean: %v", fixedOnlyViolations(table))
	}
	planted := strings.ReplaceAll(table, "TightLoadRetainMessages", "TightLoadRetainMessagez") + "\nfunc LooseSaveMessages() {}\n"
	got := strings.Join(fixedOnlyViolations(planted), "\n")
	if !strings.Contains(got, "carries LooseSaveMessages") || !strings.Contains(got, "lacks TightLoadRetainMessages") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the planted verbs were not both named:\n%s", got)
	}
}

// TestGoWireIntegrityMessageFormNegativeControl: TestGoWireIntegrityMessageForm
// with the batch trailer's CRC-32C comparison planted out of the sealed open,
// so a flipped byte reaches the body reader.
func TestGoWireIntegrityMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runGeneratedEdited(t, integrityMessageSchema, integrityMessageProbe, func(files map[string][]byte) {
		sabotageGenerated(t, files, "integrity trailer", [2]string{"binary.LittleEndian.Uint32(data[n:]) != tableMessageIntegritySum(data[:n])", "false"})
	})
	requireRed(t, "the trailer check planted out", out, err, " ^ 0x")
}
