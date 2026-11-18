//go:build schema_leg_ruby

// The Ruby packet wire through the compiler's driver (docs/plans/ruby-leg.md,
// R3 and R4), in a build with the schema_leg_ruby tag:
// `go test -tags schema_leg_ruby ./compiler -run Ruby` (make test-ruby).
package compiler

import (
	"slices"
	"strings"
	"testing"
)

// TestRubyTargetIsRegistered: the tagged build carries the ruby target,
// registered from its own init, with its table carries set registered empty.
func TestRubyTargetIsRegistered(t *testing.T) {
	t.Parallel()
	if !slices.Contains(New().Targets(), "ruby") {
		t.Fatalf("the schema_leg_ruby build lists no ruby target: %v", New().Targets())
	}
	got, ok := Carried("ruby")
	if !ok {
		t.Fatal("the ruby target registered no carries set (compiler/carries_ruby.go)")
	}
	if len(got) != 0 {
		t.Fatalf("the ruby packet target carries %v; it carries no table construct until R5/R6", got)
	}
}

// TestRubyGeneratesAnEmptyUnitThroughTheDriver: a schema file that declares
// nothing but its package, loaded and generated through the public driver, is
// the unit module, its RBS signature, its file and the runtime.
func TestRubyGeneratesAnEmptyUnitThroughTheDriver(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Hello.schema": "package hello\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "ruby", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"hello.rb", "hello.rbs", "hello/Hello.rb", "hello/_schema.rb", "hello/serialize.rb", "hello/serialize/core.rb"}
	var got []string
	for name := range files {
		got = append(got, name)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("generate --lang ruby emitted %v, want %v", got, want)
	}
}

// TestRubyCarriesThePacketWireThroughTheDriver: packet declarations generate
// cleanly through the driver, including keyed packet arrays, while tables
// remain refused by name until R5/R6.
func TestRubyCarriesThePacketWireThroughTheDriver(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\nconst MaxPlayers = 64\n\ntype Player\n{\n    health int16\n    alive bool\n    level uint8 = 1 | min = 1, max = 99\n    small bits(9)\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "ruby", nil)
	if err != nil {
		t.Fatalf("the packet wire was refused: %v", err)
	}
	if !strings.Contains(string(files["game/Game.rb"]), "class Player") {
		t.Fatal("generate --lang ruby emitted no class Player in game/Game.rb")
	}

	u, err = c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\nenum Team { Red, Blue }\n\ntype Probe\n{\n    score [Team]uint8\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err = c.Generate(u, "ruby", nil)
	if err != nil {
		t.Fatalf("keyed packet array refused: %v", err)
	}
	if !strings.Contains(string(files["game/Game.rb"]), "class Probe") {
		t.Fatal("keyed packet declaration was not emitted")
	}

	u, err = c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\ntable Fleet {\n  size uint8\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(u, "ruby", nil); err == nil || !strings.Contains(err.Error(), "and the ruby table backend is a named follow-on") {
		t.Errorf("table declaration: got %v, want refusal naming follow-on", err)
	}
}
