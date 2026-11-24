package pythontable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// A map key's length is bounded by the map's own string(N) and by nothing
// else: the 255-byte cap is for schema names (docs/SPEC-TABLES.md §16.3), and
// the Go engine takes a 300-byte key under string(400). The JSON read also
// takes BYTES (§16.3) and validates UTF-8 itself.
func TestPythonJsonMapKeyBoundAndBytesEntry(t *testing.T) {
	t.Parallel()
	const schema = `package jsonkeybytes
table Root {
    m map[string(400)]int32
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonkeybytes import Root, Refused
key = 'x' * 300
assert Root.from_json('{"m":{"' + key + '":7}}').m == {key: 7}
assert Root.from_json(Root.from_json('{"m":{"' + key + '":7}}').to_json()).m == {key: 7}
try:
    Root.from_json('{"m":{"' + 'x' * 401 + '":7}}')
except Refused:
    pass
else:
    raise AssertionError('accepted a key over string(400)')
text = '{"m":{"é\U0001d11e":3}}'
want = {'é\U0001d11e': 3}
for wrap in (bytes, bytearray, memoryview):
    assert Root.from_json(wrap(text.encode('utf-8'))).m == want, wrap
for bad in (b'{"m":{"\xff":1}}', b'{"m":{"\xed\xa0\x80":1}}', b'{"m":{"\xc0\x80":1}}'):
    for wrap in (bytes, bytearray, memoryview):
        try:
            Root.from_json(wrap(bad))
        except Refused:
            continue
        raise AssertionError('accepted bad UTF-8 bytes ' + repr(bad))
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
	if out, err := pyrun.Analyze(t, dir, "jsonkeybytes/_schema/json_scan.py"); err != nil {
		t.Fatalf("mypy --strict: %s", out)
	}
}

// The write's depth cap counts every container the read does
// (docs/SPEC-TABLES.md §16.14 W2): a chain of 127 links puts each node's empty
// array, map or None union at depth 128 and writes; 128 links put it at depth
// 129, which the read refuses, so the write refuses it too, empty [] and {}
// included. Each container is its own unit, so each refusal is that
// container's own.
func TestPythonJsonWriteDepthCountsEveryContainer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, field, text string }{
		{"array", "items []int32", `"items": []`},
		{"map", "m map[string(8)]int32", `"m": {}`},
		{"union", "pick Pick", `"pick": {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			union := ""
			if tc.name == "union" {
				union = "union Pick {\n    n int32\n    s string(8)\n}\n"
			}
			schema := "package jsonwdepth\n" + union + `table Node {
    link *Node
    ` + tc.field + `
}`
			files, err := Generate(loadUnit(t, schema))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + `from jsonwdepth import Node, InvalidValue
head = Node()
cur = head
for _ in range(127):
    nxt = Node()
    cur.link = nxt
    cur = nxt
out = head.to_json()
assert '` + tc.text + `' in out, 'the container at depth 128 was not written'
assert Node.from_json(out).to_json() == out
cur.link = Node()
try:
    head.to_json()
except InvalidValue:
    pass
else:
    raise AssertionError('a ` + tc.name + ` at depth 129 was written')
print('ok')
`
			inputs := map[string][]byte{"main.py": []byte(probe)}
			if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
				t.Fatalf("current: %q", got)
			}
			if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
				t.Fatalf("floor: %q", got)
			}
		})
	}
}
