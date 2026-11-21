// THE MESSAGE CODEC TABLES, held to the manifest and to the capability lists
// (declared.go). A driver binds each (unit, root) the `message` surface reads
// in a table of its own; a row the table does not name is answered
// `<case>.absent` (or, in C++, aborts the surface), which is how a missing
// entry passed as a missing feature: the message-drift rows reach the driver
// as ordinary `message` lines, and a table kept to the `message` lines alone
// never saw them. This is a source test: it reads the manifest, each driver's
// source and each leg's absent.txt, and runs nothing.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// messageCodecTables is where each leg with an explicit message codec table
// keeps it, and the pattern one entry's (unit, root) is spelled by.
var messageCodecTables = []struct {
	lang, source string
	entry        *regexp.Regexp
}{
	{"cpp", "test/conformance/cpp/main.cpp", regexp.MustCompile(`MSGCODEC\( "([a-z0-9]+)", [a-z0-9_]+, ([A-Za-z0-9]+) \),`)},
	{"js", "test/conformance/js/main.mjs", regexp.MustCompile(`\["([a-z0-9]+)", "([A-Za-z0-9]+)", "[^"]+", "[^"]+"\],`)},
	{"dart", "test/conformance/dart/main.dart", regexp.MustCompile(`'([a-z0-9]+) ([A-Za-z0-9]+)': _messageRow<`)},
	{"rust", "test/conformance/rust/src/main.rs", regexp.MustCompile(`\("([a-z0-9]+)", "([A-Za-z0-9]+)"\) => message_round_trip!`)},
}

// messageTableSection cuts a driver's source down to its `message` surface's
// table, so an entry of some other table (a fuzz roster) cannot stand in.
func messageTableSection(lang string, src []byte) ([]byte, error) {
	var start, end string
	switch lang {
	case "cpp":
		start, end = "static const MsgCodec msg_codecs[] = {", "};"
	case "js":
		start, end = "const messageRows = [", "];"
	case "dart":
		start, end = "final Map<String, MessageCodec> messageRuns = {", "\n};"
	case "rust":
		start, end = "fn message_codec(", "\n}"
	default:
		return nil, fmt.Errorf("no table section for %s", lang)
	}
	i := bytes.Index(src, []byte(start))
	if i < 0 {
		return nil, fmt.Errorf("no %q", start)
	}
	j := bytes.Index(src[i:], []byte(end))
	if j < 0 {
		return nil, fmt.Errorf("%q is not closed by %q", start, end)
	}
	return src[i : i+j], nil
}

// messageRowKey is one (unit, root) the message surface reads and the cases
// (as the driver sees them: a drift row is a `message` line of its own name)
// that read it.
type messageRowKey struct{ unit, root string }

// manifestMessageRows reads, from the committed manifest, every message case
// on a fixed root by the (unit, root) its driver must bind: a `message` row by
// its connection's unit, a `message-drift` row by its reader unit (run.go's
// deriveManifest hands it to the driver as a connection of that unit).
func manifestMessageRows(manifest []byte) (map[messageRowKey][]string, error) {
	connUnit := map[string]string{}
	type pending struct{ conn, root, name string }
	var messages []pending
	out := map[messageRowKey][]string{}
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "connection":
			if len(f) < 3 {
				return nil, fmt.Errorf("short connection row %q", sc.Text())
			}
			connUnit[f[1]] = f[2]
		case "message":
			if len(f) != 6 {
				return nil, fmt.Errorf("message row is not six fields: %q", sc.Text())
			}
			if f[5] != "-" { // `-` is a variable root's named refusal, scored without a codec
				messages = append(messages, pending{conn: f[2], root: f[3], name: f[1]})
			}
		case "message-drift":
			if len(f) != 8 {
				return nil, fmt.Errorf("message-drift row is not eight fields: %q", sc.Text())
			}
			k := messageRowKey{f[2], f[4]}
			out[k] = append(out[k], f[1])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, m := range messages {
		unit, ok := connUnit[m.conn]
		if !ok {
			return nil, fmt.Errorf("message row %s names no connection %s", m.name, m.conn)
		}
		k := messageRowKey{unit, m.root}
		out[k] = append(out[k], m.name)
	}
	return out, nil
}

// TestDriverMessageCodecTablesCoverTheManifest: every (unit, root) the
// manifest's message rows read is in each leg's table, or every case on it is
// declared ABSENT in that leg's capability list. The reference leg has no
// list to fall back on.
func TestDriverMessageCodecTablesCoverTheManifest(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	manifest, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := manifestMessageRows(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no message rows: the parse is wrong")
	}
	for _, table := range messageCodecTables {
		src, err := os.ReadFile(filepath.Join(root, table.source))
		if err != nil {
			t.Fatal(err)
		}
		section, err := messageTableSection(table.lang, src)
		if err != nil {
			t.Fatalf("%s: %v", table.source, err)
		}
		bound := map[messageRowKey]bool{}
		for _, m := range table.entry.FindAllSubmatch(section, -1) {
			bound[messageRowKey{string(m[1]), string(m[2])}] = true
		}
		if len(bound) == 0 {
			t.Fatalf("%s: the message table binds nothing: the pattern is wrong", table.source)
		}
		decl, err := readDeclaredAbsences(filepath.Join(root, defaultDriversDir, table.lang, declaredAbsenceFile))
		if err != nil {
			t.Fatal(err)
		}
		for k, cases := range rows {
			if bound[k] {
				continue
			}
			for _, name := range cases {
				if _, ok := decl.covers("message", name); !ok {
					t.Errorf("%s binds no message codec for %s %s, and %s/%s does not declare the message row %s ABSENT",
						table.source, k.unit, k.root, table.lang, declaredAbsenceFile, name)
				}
			}
		}
	}
}
