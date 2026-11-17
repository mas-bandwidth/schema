//go:build schema_leg_ruby

// The Ruby skeleton through the compiler's driver (docs/plans/ruby-leg.md,
// R3), in a build with the schema_leg_ruby tag:
// `go test -tags schema_leg_ruby ./compiler -run Ruby` (make test-ruby).
package compiler

import (
	"slices"
	"strings"
	"testing"
)

// TestRubyTargetIsRegistered: the tagged build carries the ruby target,
// registered from its own init, with its carries set registered empty.
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
		t.Fatalf("the ruby skeleton carries %v; it carries nothing until a layer registers a construct", got)
	}
}

// TestRubyGeneratesAnEmptyUnitThroughTheDriver: a schema file that declares
// nothing but its package, loaded and generated through the public driver, is
// the unit module, its file and the runtime.
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
	want := []string{"hello.rb", "hello/Hello.rb", "hello/_schema.rb"}
	var got []string
	for name := range files {
		got = append(got, name)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("generate --lang ruby emitted %v, want %v", got, want)
	}
}

// TestRubyRefusesADeclarationThroughTheDriver: a declaration the skeleton
// does not carry is refused by name at compile time, through the driver,
// naming the layer that carries it; so is a table, by the shared chain.
func TestRubyRefusesADeclarationThroughTheDriver(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ src, want string }{
		{"package game\n\nconst MaxPlayers = 64\n", "Game.schema declares const MaxPlayers, and the Ruby target does not carry constants yet (docs/plans/ruby-leg.md, R4 the packet wire)"},
		{"package game\n\ntype Player {\n  hp uint8\n}\n", "Game.schema declares type Player, and the Ruby target does not carry types yet"},
		{"package game\n\ntable Fleet {\n  size uint8\n}\n", "unit declares tables (Fleet)"},
		{"package game\n\ntable Fleet {\n  size uint8\n}\n", "and the ruby table backend is a named follow-on"},
	} {
		comp := New()
		u, err := comp.Load(writeSchemas(t, map[string]string{"Game.schema": c.src}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = comp.Generate(u, "ruby", nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want a refusal containing %q", c.src, err, c.want)
		}
	}
}
