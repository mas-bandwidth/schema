package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #759: packet C stored bool as int, four bytes, while table C stored
// uint8_t. A table of a packet type with two adjacent bools then failed its
// own offsetof assert (firing at 4, layout says 1). A trailing-only bool can
// hide behind padding. Storage is always uint8_t so adding a table does not
// move packet headers. serialize_read_bool still takes int *.

const issue759Packet = `package probe

type Pair
{
    moving bool
    firing bool
    flags  [2]bool
}
`

const issue759WithTable = issue759Packet + `
table Pairs
{
    item Pair
}
`

func TestIssue759PacketBoolLayoutAgreesWithTable(t *testing.T) {
	u := unitFromSource(t, issue759WithTable)
	c := New()
	files, err := c.Generate(u, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}

	header := string(files["Probe.h"])
	for _, want := range []string{
		"uint8_t moving;",
		"uint8_t firing;",
		"uint8_t flags[2];",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("packet bool storage must be uint8_t: want %q in:\n%s", want, header)
		}
	}
	if strings.Contains(header, "int firing;") {
		t.Error("packet bool storage must not be int")
	}

	table := string(files["ProbeTable.h"])
	if !strings.Contains(table, "offsetof( Pair, firing ) == 1") {
		t.Errorf("table layout must keep offsetof(Pair, firing)==1; got:\n%s", table)
	}
	if strings.Contains(table, "offsetof( Pair, firing ) == 4") {
		t.Error("do not weaken the adjacent-bool layout assert to hide int storage")
	}

	wire := string(files["ProbeWire.h"])
	for _, want := range []string{
		"int bool_value = 0;",
		"if ( !serialize_read_bool( stream, &bool_value ) )",
		"value->firing = (uint8_t) bool_value;",
		"value->flags[i] = (uint8_t) bool_value;",
		"serialize_write_bool( stream, value->firing )",
		"serialize_write_bool( stream, value->flags[i] )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("bool codec shape: want %q in:\n%s", want, wire)
		}
	}
	if strings.Contains(wire, "serialize_read_bool( stream, &value->firing )") ||
		strings.Contains(wire, "serialize_read_bool( stream, &value->flags[i] )") {
		t.Error("serialize_read_bool must not take the address of uint8_t storage")
	}

	runCTableWireProbe(t, u, `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    CHECK( offsetof( Pair, moving ) == 0 );
    CHECK( offsetof( Pair, firing ) == 1 );
    CHECK( offsetof( Pair, flags ) == 2 );
    CHECK( sizeof( ( (Pair *) 0 )->firing ) == 1 );
    return 0;
}
`)
	issue759SyntaxCheckWire(t, files)
}

func TestIssue759PacketBoolUnchangedWhenATableIsAdded(t *testing.T) {
	c := New()
	without, err := c.Generate(unitFromSource(t, issue759Packet), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := c.Generate(unitFromSource(t, issue759WithTable), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	packet := string(without["Probe.h"])
	if !strings.Contains(packet, "uint8_t firing;") {
		t.Fatalf("packet-only bool storage must already be uint8_t:\n%s", packet)
	}
	for name, data := range without {
		if strings.Contains(name, "View") {
			continue
		}
		got, ok := with[name]
		if !ok {
			t.Errorf("packet file %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("packet file %s changed when a table was added — bool storage must not gate on table closure", name)
		}
	}
}

func issue759SyntaxCheckWire(t *testing.T, files map[string][]byte) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C syntax check requires cc")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	serializeC := filepath.Join(filepath.Dir(root), "serialize.c")
	if _, err := os.Stat(filepath.Join(serializeC, "serialize.h")); err != nil {
		t.Skip("serialize.c runtime header not at the sibling path")
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	tu := filepath.Join(dir, "wire.c")
	if err := os.WriteFile(tu, []byte("#include \"ProbeWire.h\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-fsyntax-only", "-I", dir, "-I", serializeC, tu,
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("wire syntax: %v\n%s", err, out)
	}
}
