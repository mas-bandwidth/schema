// The cross-target gate on arrays of unions: C++, C and C# carry the fixed
// shapes, and targets without them refuse the unit by name.
package compiler

import (
	"strings"
	"testing"
)

// unionArraySrc holds an array of unions whose arms are TYPES, so the
// union-array refusal speaks for itself — a table arm would trip
// refuseTableArms first and shadow it.
const unionArraySrc = `package probe

type Ping
{
    nonce uint32
}

type Pong
{
    nonce uint32
}

union Reply
{
    ping Ping
    pong Pong
}

fixed table Log
{
    history [..4]Reply
    undo    [2]Reply
}
`

// TestUnionArraysCarriers: C++, C#, Go and Squirrel emit the table sources for
// a unit whose closure holds an array of unions; targets without the form
// refuse the UNIT, naming the fields, the carrier and the flag that selects
// it — a fixed-class codec that never met the element must not be emitted.
func TestUnionArraysCarriers(t *testing.T) {
	u := unitFromSource(t, unionArraySrc)
	c := New()
	for _, target := range c.Targets() {
		if target == "cpp" || target == "c" || target == "cs" || target == "dart" || target == "go" || target == "js" || target == "rust" || target == "java" || target == "lobster" || target == "squirrel" {
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang %s refused the supported shape: %v", target, err)
			}
			if target == "java" {
				if _, ok := files["LogVariable.java"]; !ok {
					t.Fatalf("--lang java emitted no table source (LogVariable.java)")
				}
				continue
			}
			if target == "rust" {
				if _, ok := files["probe_records.rs"]; !ok {
					t.Fatalf("--lang rust emitted no table source")
				}
				continue
			}
			if target == "squirrel" {
				if _, ok := files["ProbeTables.nut"]; !ok {
					t.Fatalf("--lang squirrel emitted no table source (ProbeTables.nut)")
				}
				continue
			}
			suffix := "h"
			if target != "cpp" {
				suffix = target
			}
			name := "ProbeTable." + suffix
			if target == "dart" {
				name = "ProbeFixed.dart" // the Dart table backend names a fixed table's library <Base>Fixed.dart
			}
			_, table := files[name]
			// the JS variable class lives in <Package>Variable.js (§8.4)
			_, variable := files["ProbeVariable.js"]
			if !table && (target != "js" || !variable) {
				t.Fatalf("--lang %s emitted no table source", target)
			}
			continue
		}
		if target == "lua" {
			// the descriptor-driven runtime carries the construct: both
			// fields are array rows whose element is the union
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang lua refused the supported shape: %v", err)
			}
			code := string(files["ProbeTables.lua"])
			for _, f := range []string{"history", "undo"} {
				i := strings.Index(code, `name = "`+f+`"`)
				if i < 0 {
					t.Fatalf("--lang lua: no descriptor row for %s", f)
				}
				row, _, _ := strings.Cut(code[i:], "\n    },") // one descriptor row, as the formatter lays it out
				if !strings.Contains(row, `shape = "array"`) || !strings.Contains(row, `elem = { shape = "union", target = "Reply"`) {
					t.Errorf("--lang lua: %s is not an array of Reply unions: %s", f, row)
				}
			}
			continue
		}
		if target == "luau" {
			// the table descriptors and codecs carry the construct: one
			// <Base>Table.luau per unit file
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang luau refused the supported shape: %v", err)
			}
			table := false
			for name := range files {
				table = table || strings.HasSuffix(name, "Table.luau")
			}
			if !table {
				t.Fatal("--lang luau emitted no Table.luau")
			}
			continue
		}
		if target == "elixir" {
			// the variable form carries the construct (docs/SPEC-TABLES.md
			// §2.3, §2.6): one Table.ex codec per unit
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang elixir refused the supported shape: %v", err)
			}
			if _, ok := files["Table.ex"]; !ok {
				t.Fatal("--lang elixir emitted no Table.ex")
			}
			continue
		}
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("--lang %s accepted a unit with an array of unions in a table closure — it must refuse by name", target)
			}
			for _, want := range []string{"array of unions", "Log.history", "Log.undo", "--lang cpp"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
				}
			}
		})
	}
}
