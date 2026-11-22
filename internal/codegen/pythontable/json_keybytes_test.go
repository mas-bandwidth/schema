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
