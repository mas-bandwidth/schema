package python

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// textRow is one buffer_size = 16 row of the shared serialize string corpus
// (testdata/conformance/text/string.txt), the rows test/packet-text's
// Narrow.schema reproduces with its string(15) field.
type textRow struct {
	name, wire, payload string
	bits                int
	refused             bool
}

// verdict is the line the packet-text driver must print for the row.
func (r textRow) verdict() string {
	if r.refused {
		return "REFUSE"
	}
	return fmt.Sprintf("OK %d %s %d %s", r.bits, r.payload, r.bits, r.wire)
}

func textCorpus(t *testing.T, root string) []textRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "text", "string.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	packed := func(s string) string {
		s = strings.ToLower(strings.Join(strings.Fields(s), ""))
		if s == "" {
			return "-"
		}
		return s
	}
	var rows []textRow
	var row textRow
	bound := 0
	flush := func() {
		if row.name != "" && bound == 16 {
			rows = append(rows, row)
		}
		row, bound = textRow{}, 0
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, _ := strings.Cut(line, " ")
		switch key {
		case "name":
			row.name = rest
		case "param":
			_, value, _ := strings.Cut(rest, "=")
			if bound, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				t.Fatalf("bad param %q: %v", line, err)
			}
		case "bytes":
			row.wire = packed(rest)
		case "expect":
			row.refused = rest == "refused"
			if !row.refused {
				_, value, _ := strings.Cut(rest, "=")
				row.payload = packed(value)
			}
		case "consumed":
			if row.bits, err = strconv.Atoi(rest); err != nil {
				t.Fatalf("bad consumed %q: %v", line, err)
			}
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	flush()
	return rows
}

// textCorpusMismatch runs the packet-text driver over every corpus row and
// returns the first disagreement with the pinned verdict, by name, or "".
func textCorpusMismatch(t *testing.T, python, dir string, rows []textRow) string {
	t.Helper()
	var input strings.Builder
	for _, r := range rows {
		fmt.Fprintln(&input, r.wire)
	}
	stdout, stderr, err := pyrun.TryStdin(t, python, dir, "main.py", []string{dir}, input.String())
	if err != nil {
		return fmt.Sprintf("FAILED: the packet-text driver exited: %v\n%s", err, stderr)
	}
	got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(got) != len(rows) {
		return fmt.Sprintf("FAILED: the packet-text driver returned %d rows, want %d", len(got), len(rows))
	}
	for i, r := range rows {
		if got[i] != r.verdict() {
			return fmt.Sprintf("FAILED: packet-text verdict on %s: got %q, want %q", r.name, got[i], r.verdict())
		}
	}
	return ""
}

// textTree generates test/packet-text/Narrow.schema into dir beside the
// packet-text driver.
func textTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	u := loadPythonUnit(t, filepath.Join(root, "test", "packet-text", "Narrow.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := os.ReadFile(filepath.Join(root, "test", "packet-text", "python", "main.py"))
	if err != nil {
		t.Fatal(err)
	}
	files["main.py"] = driver
	return files
}

// usesText reports whether a root's by-value closure holds a string(N) or a
// bytes(N) field.
func usesText(u *ir.Unit, root string) bool {
	seen := map[string]bool{}
	var items func([]ir.Item) bool
	var field func(*ir.Field) bool
	decl := func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		if st, ok := u.Structs[name]; ok {
			return items(st.Items)
		}
		if un, ok := u.Unions[name]; ok {
			for _, v := range un.Variants {
				if !v.Void() && field(v.F) {
					return true
				}
			}
		}
		return false
	}
	field = func(f *ir.Field) bool {
		switch fieldKind(f) {
		case "string", "bytes":
			return true
		case "type", "union":
			return decl(f.Type.Name)
		}
		return false
	}
	items = func(list []ir.Item) bool {
		for _, item := range list {
			switch item := item.(type) {
			case *ir.FieldItem:
				if field(item.F) {
					return true
				}
			case *ir.Branch:
				if items(item.Then) || items(item.Else) {
					return true
				}
			}
		}
		return false
	}
	return decl(root)
}

// textContractSchema holds the text kinds at small bounds, so a probe can
// reach every refusal by hand: a 3-bit length for string(5) and bytes(5)
// leaves lengths 6 and 7 expressible, and the declared defaults are born in
// the class (SPEC §5).
const textContractSchema = `package pytext

type Label
{
    name string(5)
}

type Payload
{
    blob bytes(5)
}

type Texts
{
    flag bool
    name string(5)
    blob bytes(5)
    greeting string(8) = "hé"
    token bytes(3) = "ab"
    if flag
    {
        tail string(4)
    }
}
`

// textContractProbe holds string(N) and bytes(N) to the leg's contract
// (docs/PORT-PYTHON.md, TEXT AND BYTES): values round-trip, every
// out-of-contract write raises InvalidValue naming the field, and every
// truncated or hostile input raises Refused.
const textContractProbe = `
import sys
from array import array
from pytext.Text import Label, Payload, Texts
from pytext import InvalidValue, Refused

def fail(msg):
    print("FAILED: " + msg)
    sys.exit(1)

v = Texts()
if v.name != "" or v.blob != b"" or v.greeting != "hé" or v.token != b"ab" or v.tail != "":
    fail("the class is not born at the declared defaults: " + repr(v))

goods = [
    ("", b"", ""),
    ("abcde", b"\x00\xff\x00\xff\x00", "wxyz"),
    ("éé", b"\x00", "€"),
    ("a\U00010000", b"abc", "\U0010ffff"),
    ("éaé", bytearray(b"\x01\x02"), ""),
    ("", memoryview(b"\x03\x04\x05"), ""),
]
for name, blob, tail in goods:
    for flag in (False, True):
        v = Texts()
        v.flag, v.name, v.blob, v.tail = flag, name, blob, tail
        try:
            data = v.write()
        except InvalidValue as exc:
            fail("a valid value was refused: " + str(exc))
        for src in (data, bytearray(data), memoryview(data)):
            r = Texts.read(src)
            if type(r.blob) is not bytes:
                fail("a read returned " + type(r.blob).__name__ + ", not bytes")
            if r.name != name or r.blob != bytes(blob):
                fail("round trip: " + repr(r))
            if r.tail != (tail if flag else ""):
                fail("the branch tail: " + repr(r))
        if r.write() != data:
            fail("write-back differs")
        for n in range(len(data)):
            try:
                Texts.read(data[:n])
            except Refused:
                pass
            else:
                fail("a truncated read of %d bytes was accepted" % n)
        try:
            Texts.read(data + b"\x00")
        except Refused:
            pass
        else:
            fail("a trailing byte was accepted")

reference = 0
with open("reference.txt") as f:
    lines = f.read().splitlines()
for line in lines:
    flag, name, blob, tail, wire = [b"" if f == "-" else bytes.fromhex(f) for f in line.split()]
    v = Texts()
    v.flag, v.name, v.blob, v.tail = flag == b"\x01", name.decode("utf-8"), blob, tail.decode("utf-8")
    if v.write() != wire:
        fail("the write differs from the Go reference: got " + v.write().hex() + ", want " + wire.hex())
    if Texts.read(wire) != v:
        fail("the Go reference wire reads back as " + repr(Texts.read(wire)))
    reference += 1
if reference == 0:
    fail("no Go reference cases")

bads = [
    ("name", 5), ("name", b"ab"), ("name", None), ("name", "abcdef"),
    ("name", "ééé"), ("name", "a\U00010000b"), ("name", "a\x00b"),
    ("name", "\x00"), ("name", "\ud800"), ("name", "a\udfff"),
    ("greeting", "ééééé"), ("tail", "abcde"),
    ("blob", "abc"), ("blob", None), ("blob", [1, 2]), ("blob", b"abcdef"),
    ("blob", bytearray(6)), ("blob", memoryview(b"abcdef")),
    ("blob", memoryview(array("H", [1]))), ("blob", memoryview(b"abcd").cast("B", (2, 2))),
    ("token", b"abcd"),
]
for attr, bad in bads:
    v = Texts()
    v.flag = True
    setattr(v, attr, bad)
    try:
        v.write()
    except InvalidValue as exc:
        if exc.path != "Texts." + attr:
            fail("the refusal of %s=%r names %s" % (attr, bad, exc.path))
    else:
        fail("an out-of-contract %s=%r was written" % (attr, bad))

def refused(cls, wire, what):
    try:
        cls.read(wire)
    except Refused:
        return
    fail("a hostile %s read was accepted: %s" % (what, wire.hex()))

def accepted(cls, wire, attr, want):
    try:
        got = getattr(cls.read(wire), attr)
    except Refused as exc:
        fail("a valid %s read was refused (%s): %s" % (cls.__name__, exc.reason, wire.hex()))
    if got != want or type(got) is not type(want):
        fail("%s read %r, want %r" % (cls.__name__, got, want))

for cls in (Label, Payload):
    refused(cls, b"", "empty")
    refused(cls, bytes([6]) + b"abcdef", "length past N")
    refused(cls, bytes([7]) + b"abcdefg", "length past N")
    refused(cls, bytes([2 | 8]) + b"ab", "nonzero padding")
    refused(cls, bytes([3]) + b"ab", "past-end payload")
refused(Label, bytes([3]) + b"a\x00b", "interior NUL")
refused(Label, bytes([1]) + b"\x00", "NUL")
refused(Label, bytes([3]) + b"\xed\xa0\x80", "surrogate")
refused(Label, bytes([2]) + b"\xc0\xaf", "overlong")
refused(Label, bytes([1]) + b"\xff", "byte ff")
refused(Label, bytes([1]) + b"\x80", "lone continuation")
refused(Label, bytes([2]) + b"\xe2\x82", "truncated sequence")
refused(Label, bytes([4]) + b"\xf4\x90\x80\x80", "above 10FFFF")
accepted(Label, bytes([3]) + b"\xe2\x82\xac", "name", "€")
accepted(Label, bytes([4]) + b"\xf4\x8f\xbf\xbf", "name", "\U0010ffff")
accepted(Label, bytes([5]) + b"abcde", "name", "abcde")
accepted(Payload, bytes([3]) + b"\x00\xed\xff", "blob", b"\x00\xed\xff")
accepted(Payload, bytes([0]), "blob", b"")
print("ok")
`

// TestPythonPacketText holds the text kinds, string(N) and bytes(N), under
// both pinned interpreters: every C++-pinned golden that uses them reads and
// writes back byte-identical, a fixture of its own writes the Go reference
// writer's bytes and reads them back, the shared serialize string corpus (the UTF-8
// refusals among it) gets its pinned verdict from the packet-text driver,
// every out-of-contract write raises InvalidValue, every truncated or
// hostile input raises Refused, and mypy --strict passes the generated tree.
func TestPythonPacketText(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	interpreters := []struct {
		name string
		fn   func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}}

	t.Run("goldens", func(t *testing.T) {
		t.Parallel()
		var pins []packetPin
		units := map[string]*ir.Unit{}
		for _, pin := range packetPins() {
			if pin.note != "" {
				continue
			}
			key := strings.Join(pin.paths, "|")
			u, ok := units[key]
			if !ok {
				abs := make([]string, len(pin.paths))
				for i, p := range pin.paths {
					abs[i] = filepath.Join(root, filepath.FromSlash(p))
				}
				u = loadPythonUnit(t, abs...)
				units[key] = u
			}
			if rootBlocker(u, pin.root) == "" && usesText(u, pin.root) {
				pins = append(pins, pin)
			}
		}
		if len(pins) == 0 {
			t.Fatal("no C++-pinned packet golden uses string(N) or bytes(N) with only carried kinds; the gate would prove nothing")
		}
		sort.Slice(pins, func(i, j int) bool { return pins[i].name < pins[j].name })
		for _, pin := range pins {
			u := units[strings.Join(pin.paths, "|")]
			t.Run(pin.name, func(t *testing.T) {
				t.Parallel()
				pruned := rootClosure(u, pin.root)
				files, err := Generate(pruned)
				if err != nil {
					t.Fatalf("generate %s: %v", pin.root, err)
				}
				base := pruned.DeclFile[pin.root]
				intField, boolField, intMin, intMax := topLevelFields(pruned, pin.root)
				probe := packetProbe(pruned, pin.root, base, intField, boolField, intMin, intMax)
				pinBytes, err := os.ReadFile(pinFile(root, pin.name))
				if err != nil {
					t.Fatal(err)
				}
				for _, run := range interpreters {
					t.Run(run.name, func(t *testing.T) {
						t.Parallel()
						dir := t.TempDir()
						writeTree(t, dir, files)
						out, stderr, err := pyrun.Try(t, run.fn(t), dir, "main.py", map[string][]byte{
							"main.py":   []byte(probe),
							"input.bin": pinBytes,
						})
						if err != nil || strings.TrimSpace(out) != "ok" {
							t.Fatalf("%s under %s: %v\n%s\n%s", pin.name, run.name, err, out, stderr)
						}
					})
				}
			})
		}
	})

	t.Run("corpus", func(t *testing.T) {
		t.Parallel()
		rows := textCorpus(t, root)
		utf8 := 0
		for _, r := range rows {
			if r.refused && !strings.Contains(r.name, "refuse-nul-") && !strings.Contains(r.name, "past-end") && !strings.Contains(r.name, "padding") {
				utf8++
			}
		}
		if len(rows) < 33 || utf8 < 13 {
			t.Fatalf("the buffer_size 16 corpus has %d rows and %d UTF-8 refusals, want at least 33 and 13", len(rows), utf8)
		}
		for _, run := range interpreters {
			t.Run(run.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, textTree(t, root))
				if msg := textCorpusMismatch(t, run.fn(t), dir, rows); msg != "" {
					t.Fatal(msg)
				}
			})
		}
	})

	t.Run("contract", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		path := filepath.Join(src, "Text.schema")
		if err := os.WriteFile(path, []byte(textContractSchema), 0o644); err != nil {
			t.Fatal(err)
		}
		u := loadPythonUnit(t, path)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range interpreters {
			t.Run(run.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				out, stderr, err := pyrun.Try(t, run.fn(t), dir, "main.py", map[string][]byte{
					"main.py":       []byte(pyrun.ImportPrelude + textContractProbe),
					"reference.txt": []byte(textReference()),
				})
				if err != nil || strings.TrimSpace(out) != "ok" {
					t.Fatalf("under %s: %v\n%s\n%s", run.name, err, out, stderr)
				}
			})
		}
		t.Run("mypy", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, err := pyrun.Analyze(t, dir, u.Package)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("mypy --strict: %v\n%s", err, out)
			}
		})
	})
}

// TestPythonPacketTextNegativeControl plants a fault in a copy of the
// generated tree and requires the corpus gate to go red by name: once in the
// generated reader (the string's buffer cut from 16 to 8, a three-bit length
// prefix: a wider one reads the same bytes, because the align absorbs it) and once in the runtime the tree carries (the
// strict UTF-8 decode replaced by Latin-1, which accepts every byte).
func TestPythonPacketTextNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	rows := textCorpus(t, root)
	plants := []struct {
		name, file, from, to string
	}{
		{"reader-bound", "packettext/Narrow.py", "_s_serialize_string(_s_rs, 16)", "_s_serialize_string(_s_rs, 8)"},
		{"runtime-utf8", "packettext/_schema/serialize/_core.py", `text = payload.decode("utf-8")`, `text = payload.decode("latin-1")`},
	}
	for _, plant := range plants {
		t.Run(plant.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := textTree(t, root)
			text := string(files[plant.file])
			if !strings.Contains(text, plant.from) {
				t.Fatalf("%s has no %q to plant over", plant.file, plant.from)
			}
			files[plant.file] = []byte(strings.ReplaceAll(text, plant.from, plant.to))
			writeTree(t, dir, files)
			msg := textCorpusMismatch(t, pyrun.Interpreter(t), dir, rows)
			if !strings.Contains(msg, "FAILED: packet-text verdict on ") {
				t.Fatalf("the planted %s fault did not go red on the verdict marker:\n%s", plant.name, msg)
			}
		})
	}
}

// pinFile is a packet golden's path: testdata/wire/<name>.bin, or, for the
// packet-defaults pins, testdata/wire/packet-defaults/<name>.bin, where that
// fixture keeps its goldens beside their .bits.
func pinFile(root, name string) string {
	p := filepath.Join(root, "testdata", "wire", name+".bin")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join(root, "testdata", "wire", "packet-defaults", name+".bin")
}

// refBits is the Go reference writer for the contract fixture: serialize's
// bit order (the low bit first, bytes in order), the length of a text field
// over [0, N] in bits_required(0, N) bits, zero padding to the byte, then
// the used bytes (SPEC §4.7), and the final partial byte flushed.
type refBits struct {
	out  []byte
	bits int
}

func (w *refBits) put(v uint64, n int) {
	for i := range n {
		if w.bits%8 == 0 {
			w.out = append(w.out, 0)
		}
		if v>>i&1 == 1 {
			w.out[len(w.out)-1] |= 1 << (w.bits % 8)
		}
		w.bits++
	}
}

func (w *refBits) text(data []byte, bound int64) {
	w.put(uint64(len(data)), rangeBits(bound))
	w.put(0, (8-w.bits%8)%8)
	for _, b := range data {
		w.put(uint64(b), 8)
	}
}

func rangeBits(max int64) int {
	n := 0
	for max > 0 {
		n++
		max >>= 1
	}
	return n
}

// textReference is the reference.txt the contract probe reads: one case per
// line, flag, name, blob and tail as hex ("-" for none) and the wire the Go
// reference writer gives for Texts with greeting and token at their defaults.
func textReference() string {
	cases := []struct {
		flag             bool
		name, blob, tail string
	}{
		{false, "", "", ""},
		{true, "", "", ""},
		{true, "abcde", "\x00\xff\x00\xff\x00", "wxyz"},
		{false, "éé", "\x00", ""},
		{true, "a\U00010000", "abc", "\U0010ffff"},
		{true, "éaé", "\x01\x02", "€"},
	}
	hexOr := func(s string) string {
		if s == "" {
			return "-"
		}
		return fmt.Sprintf("%x", s)
	}
	var b strings.Builder
	for _, c := range cases {
		w := &refBits{}
		flag := "00"
		if c.flag {
			flag = "01"
			w.put(1, 1)
		} else {
			w.put(0, 1)
		}
		w.text([]byte(c.name), 5)
		w.text([]byte(c.blob), 5)
		w.text([]byte("hé"), 8)
		w.text([]byte("ab"), 3)
		if c.flag {
			w.text([]byte(c.tail), 4)
		}
		fmt.Fprintf(&b, "%s %s %s %s %s\n", flag, hexOr(c.name), hexOr(c.blob), hexOr(c.tail), hexOr(string(w.out)))
	}
	return b.String()
}
