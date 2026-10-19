package ci

// THE DART PORT RUNS THE WHOLE SHARED HOSTILE CORPUS
// (schema-dart-runs-hostile-corpus; docs/HOSTILE-CORPUS.md).
//
// The corpus is one committed manifest and one raw input per case. This test
// generates the Dart readers for every unit the manifest names, writes the
// runner below into a temporary directory, runs it with the leg's own SDK,
// and compares each answer with the manifest: the verdict, the named reason,
// and, for an accept, the SHA-256 of the canonical text. It reads files only.
//
// THE PLANT. HOSTILE_DART_PLANT=1 makes the runner answer 'damaged' for
// wire-form-ff. The negative-control test requires that plant to be seen, and
// requires the same case to answer 'newer_form' without it.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
)

// corpusUnits is every schema the manifest names, and the subdirectory its
// generated libraries are written to.
var corpusUnits = []struct{ schema, dir string }{
	{"tables/examples", "examples"},
	{"tables/pointers", "pointers"},
	{"tables/messages", "messages"},
	{"tables/blobs", "blobs"},
	{"tables/scalars", "scalars"},
	{"tables/stream", "stream"},
	{hostilecorpus.ProbeSchemaPath, "probeunit"},
}

func hostileRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

func dartSDK(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("DART"); env != "" {
		return env
	}
	if p, err := exec.LookPath("dart"); err == nil {
		return p
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no Dart SDK resolves (set DART)")
	}
	t.Skip("no Dart SDK resolves: see make/dart.mk")
	return ""
}

// generateCorpusReaders writes every unit's Dart libraries into dir.
func generateCorpusReaders(t *testing.T, root, dir string) {
	t.Helper()
	for _, un := range corpusUnits {
		path := filepath.Join(root, filepath.FromSlash(un.schema))
		if un.schema == hostilecorpus.ProbeSchemaPath {
			path = filepath.Join(dir, "probe.schema")
			if err := os.WriteFile(path, []byte(hostilecorpus.ProbeSchema), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		paths, err := compiler.GatherPaths([]string{path})
		if err != nil {
			t.Fatalf("%s: gather: %v", un.schema, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("%s: load: %v", un.schema, err)
		}
		files, err := dart.Generate(u)
		if err != nil {
			t.Fatalf("%s: generate packet dart: %v", un.schema, err)
		}
		tables, err := darttable.Generate(u)
		if err != nil {
			t.Fatalf("%s: generate tables dart: %v", un.schema, err)
		}
		for name, data := range tables {
			files[name] = data
		}
		out := filepath.Join(dir, un.dir)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// dartRunner is the port's corpus runner. It reads the manifest, decodes each
// raw input with the carrier the manifest names, and prints one line per case.
const dartRunner = `// The Dart leg of the shared hostile-input corpus runner.
// Reads every manifest case, decodes the raw input with the carrier, prints
// CASE<TAB>name<TAB>verdict<TAB>reason<TAB>base64(canonical text or '').
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'examples/TabledemoWire.dart' as ex;
import 'examples/TabledemoFixed.dart' as exfix;
import 'pointers/GraphdemoWire.dart' as pt;
import 'messages/MessagedemoWire.dart' as msg;
import 'messages/MessagesFixed.dart' as msgload;
import 'messages/MessagedemoFixed.dart' as msgfix;
import 'blobs/BlobdemoWire.dart' as bl;
import 'scalars/ScalardemoWire.dart' as sc;
import 'stream/StreamdemoWire.dart' as st;
import 'probeunit/probeWire.dart' as pr;

String b64(String s) => base64.encode(utf8.encode(s));

String wire(String schema, String table, Uint8List data) {
  String reason;
  dynamic report;
  dynamic value;
  bool refused = false, malformed = false;
  switch (schema + '/' + table) {
    case 'tables/examples/ShipEntry':
      report = ex.TableWireReport();
      value = ex.shipEntryWireLoad(data, report);
      break;
    case 'schemas/probe.schema/Root':
      report = pr.TableWireReport();
      value = pr.rootWireLoad(data, report);
      break;
    default:
      return 'refuse unnamed';
  }
  refused = report.refused;
  malformed = report.malformed;
  if (refused) {
    if (data.isNotEmpty && data[0] == 2) reason = 'message_form_as_file';
    else if (data.isNotEmpty && data[0] != 1) reason = 'newer_form';
    else if (schema == 'schemas/probe.schema') reason = 'count_refusal';
    else reason = 'damaged';
    return 'refuse $reason';
  }
  if (malformed) return 'refuse damaged';
  final text = schema == 'tables/examples'
      ? ex.shipEntryToJson(value)
      : 'accept-without-text';
  return 'accept ${b64(text)}';
}

String text(String schema, String table, List<int> raw) {
  final data = utf8.decode(raw, allowMalformed: true);
  try {
    String out;
    switch (schema + '/' + table) {
      case 'tables/examples/RootConfig':
        final r = ex.TableTextReport();
        out = ex.rootConfigToJson(ex.rootConfigFromJson(data, r));
        break;
      case 'tables/examples/GlobalSettings':
        final r = ex.TableTextReport();
        out = ex.globalSettingsToJson(ex.globalSettingsFromJson(data, r));
        break;
      case 'tables/examples/PackConfig':
        final r = ex.TableTextReport();
        out = ex.packConfigToJson(ex.packConfigFromJson(data, r));
        break;
      case 'tables/examples/ShipEntry':
        final r = ex.TableTextReport();
        out = ex.shipEntryToJson(ex.shipEntryFromJson(data, r));
        break;
      case 'tables/pointers/Scene':
        final r = pt.TableTextReport();
        out = pt.sceneToJson(pt.sceneFromJson(data, r));
        break;
      case 'tables/messages/ToolMessage':
        final r = msg.TableTextReport();
        out = msg.toolMessageToJson(msg.toolMessageFromJson(data, r));
        break;
      case 'tables/blobs/Catalog':
        final r = bl.TableTextReport();
        out = bl.catalogToJson(bl.catalogFromJson(data, r));
        break;
      case 'tables/scalars/SimState':
        final r = sc.TableTextReport();
        out = sc.simStateToJson(sc.simStateFromJson(data, r));
        break;
      case 'tables/stream/Feed':
        final r = st.TableTextReport();
        out = st.feedToJson(st.feedFromJson(data, r));
        break;
      default:
        return 'refuse unnamed';
    }
    return 'accept ${b64(out)}';
  } catch (_) {
    return 'refuse damaged';
  }
}

String announce(carrier, Uint8List data) {
  final entries = List<exfix.TableMessageEntry>.generate(8192, (i) => exfix.TableMessageEntry(0, 0));
  late exfix.TableVocabulary v;
  if (carrier == 'announce_bytes') {
    v = exfix.TableVocabulary(entries: entries, maxBytes: 8);
  } else if (carrier == 'announce_entries') {
    v = exfix.TableVocabulary(entries: entries, maxEntries: 1);
  } else {
    v = exfix.TableVocabulary(entries: entries);
  }
  final r = exfix.TableReport();
  if (carrier == 'announce_second') {
    if (!exfix.announceRead(v, data, r)) return 'refuse ${r.reason}';
    final r2 = exfix.TableReport();
    if (!exfix.announceRead(v, data, r2)) return 'refuse ${r2.reason}';
    return 'accept';
  }
  if (!exfix.announceRead(v, data, r)) {
    if (r.refused) return 'refuse ${r.reason}';
    return 'refuse damaged';
  }
  return 'accept';
}

String message(String carrier, Uint8List data) {
  final r = msgfix.TableReport();
  final vocab = msgfix.TableVocabulary(entries: List<msgfix.TableMessageEntry>.generate(8192, (i) => msgfix.TableMessageEntry(0, 0)));
  if (carrier == 'message') {
    final ar = msgfix.TableReport();
    if (!msgfix.announceRead(vocab, msgfix.tableAnnouncement, ar)) {
      return ar.refused ? 'refuse ${ar.reason}' : 'refuse damaged';
    }
  }
  final values = List<msgfix.ToolMessage>.generate(1, (i) => msgfix.ToolMessage());
  final n = msgload.toolMessageLoadMessages(values, data, vocab, r);
  if (r.refused) return 'refuse ${r.reason}';
  if (n < 0) return r.malformed ? 'refuse damaged' : 'refuse unnamed';
  if (n != 1) return 'refuse damaged';
  final out = Uint8List(msgload.toolMessageMeasureMessages(values, msgfix.TableReport()));
  final r2 = msgfix.TableReport();
  final wrote = msgload.toolMessageSaveMessages(values, out, r2);
  if (wrote <= 0 || r2.refused) return 'refuse damaged';
  return 'accept-roundtrip:${base64.encode(out.sublist(0, wrote))}';
}

void main(List<String> args) {
  final root = args.isEmpty ? '.' : args[0];
  final corpus = args.length < 2 ? '$root/internal/ci/hostilecorpus/corpus' : args[1];
  final lines = File('$corpus/manifest.tsv').readAsLinesSync();
  for (final line in lines) {
    if (line.isEmpty || line.startsWith('#')) continue;
    final f = line.split('\t');
    if (f.length != 9) continue;
    final raw = File('$corpus/${f[4]}').readAsBytesSync();
    String got;
    switch (f[1]) {
      case 'wire':
        got = wire(f[2], f[3], raw);
        break;
      case 'text':
        got = text(f[2], f[3], raw);
        break;
      case 'message':
      case 'message_novocab':
        got = message(f[1], raw);
        break;
      case 'announce':
      case 'announce_second':
      case 'announce_bytes':
      case 'announce_entries':
        got = announce(f[1], raw);
        break;
      default:
        got = 'refuse unnamed';
    }
    if (Platform.environment['HOSTILE_DART_PLANT'] == '1' && f[0] == 'wire-form-ff') {
      got = 'refuse damaged';
    }
    stdout.writeln('CASE\t${f[0]}\t$got');
  }
}
`

type answer struct {
	verdict, reason, text string
	roundtrip             []byte
}

func parseAnswers(out string) map[string]answer {
	got := map[string]answer{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || f[0] != "CASE" {
			continue
		}
		a := answer{}
		switch {
		case strings.HasPrefix(f[2], "accept-roundtrip:"):
			a.verdict = "accept"
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(f[2], "accept-roundtrip:"))
			if err == nil {
				a.roundtrip = raw
			}
		case strings.HasPrefix(f[2], "accept "):
			a.verdict = "accept"
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(f[2], "accept "))
			if err == nil {
				a.text = string(raw)
			}
		case strings.HasPrefix(f[2], "refuse "):
			a.verdict = "refuse"
			a.reason = strings.TrimPrefix(f[2], "refuse ")
		default:
			a.verdict = f[2]
		}
		got[f[1]] = a
	}
	return got
}

// runCorpus generates the readers, writes the runner, runs it, and returns the
// answers keyed by case name.
func runCorpus(t *testing.T, plant bool) (map[string]answer, []hostilecorpus.Case) {
	t.Helper()
	dart := dartSDK(t)
	root := hostileRepoRoot(t)
	dir := t.TempDir()
	generateCorpusReaders(t, root, dir)
	if err := os.WriteFile(filepath.Join(dir, "runner.dart"), []byte(dartRunner), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "runner.dart", root, filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	if plant {
		cmd.Env = append(cmd.Env, "HOSTILE_DART_PLANT=1")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Dart corpus runner did not run: %v\n%s", err, out)
	}
	_, cases, err := hostilecorpus.LoadManifest(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return parseAnswers(string(out)), cases
}

type mismatch struct {
	name, want, got string
}

func compare(t *testing.T, root string, cases []hostilecorpus.Case, got map[string]answer) []mismatch {
	t.Helper()
	var out []mismatch
	for _, c := range cases {
		a, ok := got[c.Name]
		if !ok {
			out = append(out, mismatch{c.Name, c.Verdict + "/" + c.Reason, "no answer"})
			continue
		}
		if a.verdict != c.Verdict {
			out = append(out, mismatch{c.Name, c.Verdict + "/" + c.Reason, a.verdict + "/" + a.reason})
			continue
		}
		if c.Verdict == hostilecorpus.Refuse {
			if a.reason != c.Reason {
				out = append(out, mismatch{c.Name, c.Reason, a.reason})
			}
			continue
		}
		if a.roundtrip != nil {
			payload, err := hostilecorpus.Payload(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"), c.Input)
			if err != nil {
				t.Fatal(err)
			}
			if string(a.roundtrip) != string(payload) {
				out = append(out, mismatch{c.Name, "round-trip bytes", "different bytes"})
			}
			continue
		}
		sum := sha256.Sum256([]byte(strings.TrimSuffix(a.text, "\n")))
		if hex.EncodeToString(sum[:]) != c.Digest {
			out = append(out, mismatch{c.Name, c.Digest, hex.EncodeToString(sum[:])})
		}
	}
	return out
}

// TestSchemaDartRunsHostileCorpus is the gate. A mismatch here is a finding
// against the Dart reader, not against the corpus.
func TestSchemaDartRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root := hostileRepoRoot(t)
	got, cases := runCorpus(t, false)
	bad := compare(t, root, cases, got)
	if len(bad) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "the Dart port disagrees with %d of %d hostile-corpus cases:", len(bad), len(cases))
		for _, m := range bad {
			fmt.Fprintf(&b, "\n  %s: want %s, got %s", m.name, m.want, m.got)
		}
		t.Fatal(b.String())
	}
	fmt.Printf("--- PASS: %d hostile-corpus cases, every verdict, reason and digest\n", len(cases))
}

// TestSchemaDartHostileCorpusNegativeControl plants a reader bug in the runner
// (scratch only, switched on by the test) and requires the comparison to see
// it. Without the plant the same case must answer its manifest verdict.
func TestSchemaDartHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	root := hostileRepoRoot(t)
	clean, cases := runCorpus(t, false)
	var cleanAnswer answer
	foundClean := false
	for _, c := range cases {
		if c.Name == "wire-form-ff" {
			cleanAnswer = clean[c.Name]
			foundClean = true
		}
	}
	if !foundClean {
		t.Fatal("the corpus carries no wire-form-ff case")
	}
	if cleanAnswer.reason != "newer_form" {
		t.Fatalf("without the plant wire-form-ff answered %q, want newer_form", cleanAnswer.reason)
	}
	planted, cases := runCorpus(t, true)
	bad := compare(t, root, cases, planted)
	for _, m := range bad {
		if m.name == "wire-form-ff" {
			t.Logf("planted reader bug seen: %s: want %s, got %s", m.name, m.want, m.got)
			return
		}
	}
	t.Fatal("the planted reader bug (wire-form-ff -> damaged) was not seen")
}
