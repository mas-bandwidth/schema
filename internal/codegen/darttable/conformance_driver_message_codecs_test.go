package darttable

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDartConformanceDriverHasEveryMessageCodec holds the Dart conformance
// driver's message codec table (test/conformance/dart/main.dart, messageRuns)
// to the corpus manifest: every `message` row on a fixed root and every
// `message-drift` row names a (unit, root) the driver must round-trip through
// the generated LoadMessages / SaveMessages (docs/SPEC-TABLES.md §3.3). A row
// with no codec answers ABSENT, and the leg's ticked message surface owes the
// answer, so a missing entry is caught here, without a compiler or a run (the
// C++ driver's twin is TestCppConformanceDriverHasEveryMessageCodec).
//
// It is a source test: it reads two files in the repository and nothing else.
func TestDartConformanceDriverHasEveryMessageCodec(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "dart", "main.dart"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	const open = "final Map<String, MessageCodec> messageRuns = {\n"
	start := bytes.Index(src, []byte(open))
	if start < 0 {
		t.Fatal("test/conformance/dart/main.dart has no messageRuns map: the anchor moved")
	}
	end := bytes.Index(src[start:], []byte("\n};\n"))
	if end < 0 {
		t.Fatal("messageRuns has no closing brace: the anchor moved")
	}
	table := src[start : start+end]
	want, err := messageCodecsTheManifestNeeds(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the manifest names no message rows: the parse is wrong")
	}
	for _, key := range want {
		spelled := fmt.Sprintf("'%s %s': _messageRow<", key.unit, key.root)
		if !bytes.Contains(table, []byte(spelled)) {
			t.Errorf("test/conformance/dart/main.dart messageRuns has no %s: the manifest's message row %s needs it", spelled, key.row)
		}
	}
}

type msgCodecKey struct {
	unit, root, row string
}

// messageCodecsTheManifestNeeds lists the (unit, root) of every message row on
// a fixed root (its message wire is not `-`) and every message-drift row, in
// manifest order, once each.
func messageCodecsTheManifestNeeds(manifest []byte) ([]msgCodecKey, error) {
	connUnit := map[string]string{}
	type pending struct{ conn, root, row string }
	var messages []pending
	var out []msgCodecKey
	seen := map[[2]string]bool{}
	add := func(unit, root, row string) {
		k := [2]string{unit, root}
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, msgCodecKey{unit: unit, root: root, row: row})
	}
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "connection":
			if len(f) < 3 {
				return nil, fmt.Errorf("short connection row: %q", line)
			}
			connUnit[f[1]] = f[2]
		case "message":
			if len(f) != 6 {
				return nil, fmt.Errorf("message row is not six fields: %q", line)
			}
			if f[5] != "-" {
				messages = append(messages, pending{conn: f[2], root: f[3], row: f[1]})
			}
		case "message-drift":
			if len(f) != 8 {
				return nil, fmt.Errorf("message-drift row is not eight fields: %q", line)
			}
			add(f[2], f[4], f[1])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, m := range messages {
		unit, ok := connUnit[m.conn]
		if !ok {
			return nil, fmt.Errorf("message row %s names no connection %s", m.row, m.conn)
		}
		add(unit, m.root, m.row)
	}
	return out, nil
}
