package rubytable

import "testing"

// docs/SPEC-TABLES.md §16.3: a map key is bounded by the map's own string(N)
// and the 255-byte cap is for schema names only; the JSON read takes bytes (a
// binary String) beside the text String and validates UTF-8 the same way; and
// the write's depth cap counts table values only (W2), so an empty array, map,
// union or flags value at depth 129 is written.
func TestRubyJsonMapKeyBytesAndWriteDepth(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfix
flags Caps {
    Read
    Write
}
union Pick {
    n int32
    s string(8)
}
table Node {
    link *Node
    items []int32
    m map[string(400)]int32
    caps Caps
    pick Pick
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := `# frozen_string_literal: true

require_relative 'jsonfix'

def refused?
  yield
  false
rescue Jsonfix::Refused
  true
end

# 1. a map key over 255 bytes under string(400)
key = 'x' * 300
raise 'long key refused' unless Jsonfix::Node.from_json(%({"m":{"#{key}":7}})).m == { key => 7 }
raise 'key over 400 held' unless refused? { Jsonfix::Node.from_json(%({"m":{"#{'x' * 401}":7}})) }

# 2. the bytes entry
text = %({"m":{"é\u{1d11e}":3}})
want = { "é\u{1d11e}" => 3 }
raise 'bytes refused' unless Jsonfix::Node.from_json(text.b).m == want
raise 'bytes mutated' unless text.b.encoding == Encoding::BINARY
[%({"m":{"\xff":1}}), %({"m":{"\xed\xa0\x80":1}}), %({"m":{"\xc0\x80":1}})].each do |bad|
  raise "accepted bad bytes #{bad.inspect}" unless refused? { Jsonfix::Node.from_json(bad.b) }
end

# 3. an empty container at layout depth 129 is written; a table at 129 is not
head = Jsonfix::Node.new
cur = head
128.times do
  nxt = Jsonfix::Node.new
  cur.link = nxt
  cur = nxt
end
out = head.to_json
raise 'depth 128 node did not write' unless out.include?('"items": []')
past = Jsonfix::Node.new
cur.link = past
begin
  head.to_json
  raise 'a table at depth 129 was written'
rescue Jsonfix::InvalidValue
  nil
end
puts 'ok'
`
	inputs := map[string][]byte{"probe.rb": []byte(probe)}
	if got := runBothRubyPins(t, dir, "probe.rb", inputs); got != "ok\n" {
		t.Fatalf("probe: %q", got)
	}
}
