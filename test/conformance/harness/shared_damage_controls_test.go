package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func damageControlUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Control.schema")
	if err := os.WriteFile(path, []byte("package damagecheck\ntable Node { p *Node }\nunion Pick { item uint8 }\ntable Child { a uint8\n b uint8\n p *Node\n data bytes(8) }\ntable Root { b uint8\n a uint8\n required *Node | required\n p *Node\n pick Pick\n list [..3]Child\n inner Child }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// scriptBytes deliberately bypasses firstUseOrder: controls must preserve the
// malformed boundaries and ID-table defects they are testing.
func TestDamageAwareFileSpellingControls(t *testing.T) {
	t.Parallel()
	u := damageControlUnit(t)
	for _, tc := range []struct {
		name    string
		ids     []string
		entries []uint64
		body    []byte
		want    string
	}{
		{name: "stored default", ids: []string{"a"}, body: b(1, 6, 0, 0), want: "stored-default"},
		{name: "explicit null", ids: []string{"p"}, body: b(1, 17, 0, 0), want: "explicit-null-pointer"},
		{name: "required pointer may carry null", ids: []string{"required"}, body: b(1, 17, 0, 0)},
		{name: "unknown pointer may carry null", ids: []string{"future"}, body: b(1, 17, 0, 0)},
		{name: "zero union arm", ids: []string{"pick"}, body: b(1, 15, 0, 0), want: "union-arm-zero"},
		{name: "empty node table", entries: []uint64{ir.TableNodeWireId}, body: b(1, 12, 1, 0, 0), want: "empty-node-table"},
		{name: "node table before last field", entries: []uint64{ir.TableNodeWireId, ir.TableTypeId("Node"), ir.TableWireId("a")}, body: b(1, 12, 4, 1, 2, 1, 0, 3, 6, 1, 0), want: "node-table-out-of-place"},
		// The same ID wire is legal against either declaration. A file carries no
		// build identity with which to infer its writer's declaration order.
		{name: "old a b writer against b a reader", ids: []string{"a", "b"}, body: b(1, 6, 1, 2, 6, 2, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := (framingVector{ids: tc.ids, entries: tc.entries, body: tc.body}).scriptBytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := damageAwareFileSpellings(u, u.Tables["Root"], wire)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected shapes %v", got)
				}
			} else if len(got) != 1 || !got[tc.want] {
				t.Fatalf("got %v, want only %s", got, tc.want)
			}
		})
	}
}
func TestDamageAwareBoundaries(t *testing.T) {
	t.Parallel()
	u := damageControlUnit(t)
	for _, tc := range []struct {
		name string
		ids  []string
		body []byte
		want bool
	}{
		// The first child's trailing reference cannot borrow the next child's L.
		{"child reference stops at its L", []string{"list", "a", "p"}, b(1, 14, 12, 13, 2, 4, 2, 6, 1, 128, 4, 3, 17, 0, 0, 0), true},
		// A malformed header stops its own table body: these following bytes could
		// resemble an explicit-null field, but have no independently proven framing.
		{"short header stops body", []string{"inner", "data", "p"}, b(1, 13, 7, 2, 14, 0, 3, 17, 0, 0, 0), false},
		// The same malformed body may not swallow a later, independently L-framed
		// child. Its null field is an observable spelling despite the earlier damage.
		{"short header preserves next child", []string{"list", "data", "p"}, b(1, 14, 15, 13, 2, 7, 2, 14, 0, 3, 17, 0, 0, 4, 3, 17, 0, 0, 0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := (framingVector{ids: tc.ids, body: tc.body}).scriptBytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := damageAwareFileSpellings(u, u.Tables["Root"], wire)
			if _, ok := errors.AsType[*fixtureDamage](err); !ok {
				t.Fatalf("got %v, want typed framing damage", err)
			}
			if got["explicit-null-pointer"] != tc.want {
				t.Fatalf("shapes %v, want null=%v; damage=%v", got, tc.want, err)
			}
			if got["fields-out-of-order"] {
				t.Fatal("file declaration order is not inferable")
			}
		})
	}
	// Parser coverage refusal must remain distinguishable from expected damage.
	_, err := damageAwareFrame([]byte{3})
	if _, ok := errors.AsType[*fixtureUnsupported](err); !ok {
		t.Fatalf("unsupported form: %v", err)
	}
}
func TestDamageAwareIDTableControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ids  []string
		body []byte
		want bool
	}{
		{"first use", []string{"a", "b"}, b(2, 6, 1, 1, 6, 1, 0), true},
		{"unused", []string{"a", "b"}, b(1, 6, 1, 0), true},
		{"duplicate entries", []string{"a", "a"}, b(1, 6, 1, 0), true},
		{"canonical", []string{"b", "a"}, b(1, 6, 1, 2, 6, 1, 0), false},
		{"damaged suffix may use remaining ID", []string{"a", "b"}, b(1, 6, 1, 128), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := (framingVector{ids: tc.ids, body: tc.body}).scriptBytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			fault, _ := damageAwareIDTableFault(wire)
			if (fault != "") != tc.want {
				t.Fatalf("fault %q want=%v", fault, tc.want)
			}
		})
	}
}

func TestDamageAwareReservedNodeKind(t *testing.T) {
	t.Parallel()
	u := damageControlUnit(t)
	wire, err := (framingVector{entries: []uint64{ir.TableNodeWireId}, body: b(1, 8, 1, 0, 0, 0, 0)}).scriptBytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	shapes, err := damageAwareFileSpellings(u, u.Tables["Root"], wire)
	damage, ok := errors.AsType[*fixtureDamage](err)
	if !ok || len(shapes) != 0 || len(damage.Issues) != 1 || !strings.Contains(damage.Issues[0], "node-table ID under a foreign kind") {
		t.Fatalf("shapes=%v damage=%v", shapes, err)
	}
}

func TestDamageAwareBooleanAndText(t *testing.T) {
	t.Parallel()
	u := damageControlUnit(t)
	for _, tc := range []struct {
		name   string
		body   []byte
		reason string
	}{{"bad boolean", b(1, 1, 2, 0), "invalid boolean byte"}, {"valid boolean", b(1, 1, 1, 0), ""},
        {"nan float", b(1, 10, 0, 0, 0xc0, 0x7f, 0), "non-finite float"},
        {"finite float", b(1, 10, 0, 0, 0, 0x80, 0), ""}, {"zero text", b(1, 12, 2, 'a', 0, 0), "ill-formed text"}, {"bad utf8", b(1, 12, 1, 0xff, 0), "ill-formed text"}, {"valid utf8", b(1, 12, 2, 0xc3, 0xa9, 0), ""}} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := (framingVector{ids: []string{"future"}, body: tc.body}).scriptBytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = damageAwareFileSpellings(u, u.Tables["Root"], wire)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			damage, ok := errors.AsType[*fixtureDamage](err)
			if !ok || len(damage.Issues) == 0 || !strings.Contains(damage.Issues[0], tc.reason) {
				t.Fatalf("damage=%v", err)
			}
		})
	}
}
