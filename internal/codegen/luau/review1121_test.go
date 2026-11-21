package luau

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// TestLuauStringLit pins LUAU-1's spelling (review 2026-11-21): Go's %q
// everywhere it is also Luau, and \u{X} where %q writes the \uXXXX or
// \UXXXXXXXX Luau rejects. The generators' %q verbs go through it.
func TestLuauStringLit(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"plain", `"plain"`},
		{`a "quoted" \ b`, `"a \"quoted\" \\ b"`},
		{"tab\tnl\ncr\r", `"tab\tnl\ncr\r"`},
		{"\x00\x1b\x7f", `"\x00\x1b\x7f"`},
		{"em — dash", `"em — dash"`},
		{"line sep", `"line\u{2028}sep"`},
		{"nel\u0085", `"nel\u{85}"`},
		{"astral\U000E0001", `"astral\u{e0001}"`},
		{"bad\xffbyte", `"bad\xffbyte"`},
	} {
		if got := StringLit(c.in); got != c.want {
			t.Errorf("StringLit(%q) = %s, want %s", c.in, got, c.want)
		}
	}
	if got := Sprintf("name = %q, n = %d, %5.1f%%, %q", "x ", 3, 1.5, []byte("y")); got != `name = "x\u{2028}", n = 3,   1.5%, "y"` {
		t.Errorf("Sprintf = %s", got)
	}
}

// TestLuauPacketWriteRefusesRatherThanRaises pins LUAU-4 (review
// 2026-11-21): the packet writer does what its documented contract says. A
// value of the wrong Luau type, or a float that is not finite at its width,
// makes Write<Name> return false and never raise: the guard runs before the
// serialize runtime, which traps on such a value. The lawful value writes.
func TestLuauPacketWriteRefusesRatherThanRaises(t *testing.T) {
	t.Parallel()
	u := unitFromSchemas(t, map[string]string{"Contract.schema": `package contract

type Inner
{
    x uint8
}

union Pick
{
    inner Inner
}

type Outer
{
    f    float32
    c    float32 | min = 0.0, max = 1.0, resolution = 0.01
    d    float64
    inr  Inner
    pick Pick
    xs   [2]Inner
}
`})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.luau", `
local serialize = require("./serialize")
local C = require("./Contract")
local cases: { any } = {
	{ "lawful", function(v) end },
	{ "f-string", function(v) v.F = "1" end },
	{ "f-nan", function(v) v.F = 0 / 0 end },
	{ "f-inf", function(v) v.F = math.huge end },
	{ "f-past-float32", function(v) v.F = 1e39 end },
	{ "c-string", function(v) v.C = "0.5" end },
	{ "c-nan", function(v) v.C = 0 / 0 end },
	{ "c-inf", function(v) v.C = -math.huge end },
	{ "d-string", function(v) v.D = "1" end },
	{ "d-nan", function(v) v.D = 0 / 0 end },
	{ "d-inf", function(v) v.D = math.huge end },
	{ "inr-nil", function(v) v.Inr = nil end },
	{ "inr-number", function(v) v.Inr = 3 end },
	{ "pick-nil", function(v) v.Pick = nil end },
	{ "xs-nil", function(v) v.Xs = nil end },
}
for _, c in cases do
	local v = C.NewOuter()
	c[2](v)
	local w = serialize.WriteStream.new(buffer.create(256))
	local ok, res = pcall(C.WriteOuter, w, v)
	local verdict = if not ok then "raised" elseif res then "written" else "refused"
	print(c[1] .. " " .. verdict)
end
local okNil, resNil = pcall(C.WriteOuter, serialize.WriteStream.new(buffer.create(256)), nil)
print("outer-nil " .. (if not okNil then "raised" elseif resNil then "written" else "refused"))
`)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil))), "\n") {
		k, v, _ := strings.Cut(line, " ")
		got[k] = v
	}
	for _, c := range []struct{ row, want string }{
		{"lawful", "written"},
		{"f-string", "refused"},
		{"f-nan", "refused"},
		{"f-inf", "refused"},
		{"f-past-float32", "refused"},
		{"c-string", "refused"},
		{"c-nan", "refused"},
		{"c-inf", "refused"},
		{"d-string", "refused"},
		{"d-nan", "refused"},
		{"d-inf", "refused"},
		{"inr-nil", "refused"},
		{"inr-number", "refused"},
		{"pick-nil", "refused"},
		{"xs-nil", "refused"},
		{"outer-nil", "refused"},
	} {
		if got[c.row] != c.want {
			t.Errorf("%s: %q, want %q", c.row, got[c.row], c.want)
		}
	}
}
