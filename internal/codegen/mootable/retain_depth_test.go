package mootable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retainDepthUnit is the reader for the binary nesting bound of
// docs/SPEC-TABLES.md §4 and §6.6: `T` knows one scalar, so a field this build
// cannot name is retained rather than read.
const retainDepthUnit = `package depth

table T
{
    known uint8
}
`

const retainDepthStore = 1 << 20

// depthFrameCase is one unknown root field and the framing inside it: `bodies`
// framed table bodies, the innermost carrying `inner` where it is not nil.
type depthFrameCase struct {
	name   string
	bodies int
	inner  func(*wb)
}

// depthWire writes T{ known = 1, x = <bodies nested table bodies> }, the last
// body holding the case's extra frame. Every id it names is unknown to the
// reader except `known`.
func depthWire(c depthFrameCase) []byte {
	b := &wb{}
	b.field("known", ir.TableKindU8)
	b.u8(1)
	var body func(n int)
	body = func(n int) {
		b.field("x", ir.TableKindTable)
		at := b.openLen()
		if n > 1 {
			body(n - 1)
		} else if c.inner != nil {
			c.inner(b)
		}
		b.end()
		b.closeLen(at)
	}
	body(c.bodies)
	b.end()
	return b.finish()
}

// scalarUnionArm writes a union field whose arm is a scalar: §4 counts no
// level for the union or its arm.
func scalarUnionArm(b *wb) {
	b.field("pick", ir.TableKindUnion)
	b.leb(b.ref("arm"))
	b.u8(ir.TableKindU8)
	b.leb(1)
	b.u8(7)
}

// keyedScalarSlot writes a keyed body with one scalar element: the keyed body
// is a level and the slot's element is not.
func keyedScalarSlot(b *wb) {
	b.field("slots", ir.TableKindKeyed)
	at := b.openLen()
	b.u8(ir.TableKindU8)
	b.leb(1)
	b.leb(b.ref("key"))
	slot := b.openLen()
	b.u8(9)
	b.closeLen(slot)
	b.closeLen(at)
}

// depthOut is one plain read and one retaining load and save of `T`.
type depthOut struct {
	plain, value                      bool
	retained, lost, unknown, saveLost int
	save                              []byte
}

func depthProbe(g *generatedUnit) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() + mooHex +
		fmt.Sprintf(`local report = L.newReport()
local plain = L.load("T", inputs.wire, L.newReport())
local store = R.newStore(%d)
local v = R.LoadRetain("T", inputs.wire, report, store)
print(tostring(plain ~= nil) .. " " .. tostring(v ~= nil) .. " " .. tostring(report.retained) .. " " .. tostring(report.retainLost) .. " " .. tostring(report.unknown))
if v == nil then return end
local before = report.retainLost
local out = R.SaveRetain("T", v, store, report)
if out == nil then print("SAVENIL") return end
print(hex(out))
print(report.retainLost - before)
`, retainDepthStore)
}

func mooDepth(t *testing.T, g *generatedUnit, wire []byte) depthOut {
	t.Helper()
	out := g.run(t, depthProbe(g), map[string][]byte{"wire": wire})
	lines := strings.Split(out, "\n")
	var o depthOut
	if _, err := fmt.Sscanf(lines[0], "%t %t %d %d %d", &o.plain, &o.value, &o.retained, &o.lost, &o.unknown); err != nil {
		t.Fatalf("moo printed %q", out)
	}
	if len(lines) < 2 || !o.value || lines[1] == "SAVENIL" {
		return o
	}
	b, err := hex.DecodeString(lines[1])
	if err != nil || len(lines) < 3 {
		t.Fatalf("moo printed %q", out)
	}
	o.save = b
	o.saveLost, _ = strconv.Atoi(lines[2])
	return o
}

// TestMooRetainKeepsAFieldNested128 holds the retaining read to §4's binary
// bound (docs/SPEC-TABLES.md §6.6): an unknown root field nested 128 framed
// levels is retained and written back byte for byte, 129 is refused by the
// plain read, and the frame kinds §4 counts differently — a scalar union arm
// and a keyed slot's scalar element — keep the Go engine's verdict.
func TestMooRetainKeepsAFieldNested128(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Depth.schema": retainDepthUnit})
	cases := []depthFrameCase{
		{name: "64 bodies", bodies: 64},
		{name: "65 bodies", bodies: 65},
		{name: "128 bodies", bodies: 128},
		{name: "scalar union arm at 128 bodies", bodies: 128, inner: scalarUnionArm},
		{name: "keyed scalar element at 127 bodies", bodies: 127, inner: keyedScalarSlot},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			wire := depthWire(c)
			want := goRetain(t, g.unit, "T", wire, retainDepthStore, -1)
			got := mooDepth(t, g, wire)
			if !got.plain {
				t.Fatalf("the plain read refused a wire inside the bound")
			}
			if !got.value || got.retained != 1 || got.lost != 0 {
				t.Fatalf("the retaining load did not keep the field: value=%v retained=%d lost=%d", got.value, got.retained, got.lost)
			}
			if !bytes.Equal(got.save, wire) {
				t.Fatalf("the retaining save did not write the field back byte for byte")
			}
			if !want.value || want.retained != got.retained || want.lost != got.lost || want.unknown != got.unknown || !bytes.Equal(want.save, got.save) {
				t.Fatalf("moo and the Go engine disagree: moo=%+v go=%+v", got, want)
			}
		})
	}
	t.Run("129 bodies", func(t *testing.T) {
		wire := depthWire(depthFrameCase{bodies: 129})
		if got := mooDepth(t, g, wire); got.plain || got.value {
			t.Fatalf("the plain read or the retaining load answered 129 framed levels: plain=%v value=%v", got.plain, got.value)
		}
		if want := goRetain(t, g.unit, "T", wire, retainDepthStore, -1); want.value {
			t.Fatal("the Go engine answered a wire past the binary bound")
		}
	})
}

// TestMooRetainKeepsAFieldNested128NegativeControl puts the cap of 64 back in
// an emitted table_retain.moo and requires the 65-body row red.
func TestMooRetainKeepsAFieldNested128NegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Depth.schema": retainDepthUnit})
	wire := depthWire(depthFrameCase{name: "65 bodies", bodies: 65})
	if got := mooDepth(t, g, wire); !got.value || got.lost != 0 || !bytes.Equal(got.save, wire) {
		t.Fatalf("the control is not clean: %+v", got)
	}
	path := filepath.Join(g.dir, "table_retain.moo")
	src := string(g.files["table_retain.moo"])
	const anchor = "local DEPTH_CAP = 128"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the depth-cap anchor is not in table_retain.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "local DEPTH_CAP = 64", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mooDepth(t, g, wire); got.value && got.lost == 0 && bytes.Equal(got.save, wire) {
		t.Fatal("the 65-body field is still kept with the cap at 64")
	}
}
