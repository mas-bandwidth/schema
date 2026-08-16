// The multi-file dispatch test: messages and objects spread across schema
// files are legal (SPEC §2 — the aspect layout is never compiler-enforced),
// so the unit-level dispatch surface must be emitted exactly ONCE per unit in
// every target, or the generated package/TU cannot compile.
package golang

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/parser"
)

func multiFileUnit(t *testing.T) *ir.Unit {
	t.Helper()
	sources := map[string]string{
		"MessagesA.schema": "package t\nmessage Ping { x uint8 }\nobject Rock { size uint8 [interpolate, min = 0, max = 100] }\n",
		"MessagesB.schema": "package t\nmessage Pong { y uint8 }\nobject Tree { size uint8 [interpolate, min = 0, max = 100] }\n",
	}
	var files []check.SourceFile
	for name, src := range sources {
		f, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
			Bytes: []byte(src), AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func countAcross(files map[string][]byte, needle string) int {
	n := 0
	for _, src := range files {
		n += strings.Count(string(src), needle)
	}
	return n
}

func TestDispatchSurfaceEmittedOnce(t *testing.T) {
	u := multiFileUnit(t)

	goFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"type MessageType ", "type Message interface", "type MessageStorage struct",
		"func WriteMessage(", "func ReadMessage(", "func WriteMessageType(",
		"type ObjectType ", "func WriteObjectType(",
	} {
		if got := countAcross(goFiles, needle); got != 1 {
			t.Errorf("Go: %q emitted %d times across the unit — the package cannot compile unless it is exactly once", needle, got)
		}
	}
	// both messages' dispatch methods live with the surface, in the owner file
	if got := countAcross(goFiles, ") MessageType() MessageType {"); got != 2 {
		t.Errorf("Go: expected 2 dispatch methods (one per message), got %d", got)
	}

	rustFiles, err := rust.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"pub struct MessageType(", "pub enum Message {", "pub fn write_message(", "pub fn read_message(",
		"pub fn write_message_type(", "pub struct ObjectType(", "pub fn write_object_type(",
	} {
		if got := countAcross(rustFiles, needle); got != 1 {
			t.Errorf("Rust: %q emitted %d times across the unit — the crate cannot compile unless it is exactly once", needle, got)
		}
	}
	// both messages ride the one dispatch enum, in the owner file
	if got := countAcross(rustFiles, "(value) => {"); got != 2 {
		t.Errorf("Rust: expected 2 write dispatch arms (one per message), got %d", got)
	}

	csFiles, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"public enum MessageType", "public abstract class Message", "public sealed class MessageStorage",
		"public static bool WriteMessage(", "public static bool ReadMessage(", "public static bool WriteMessageType(",
		"public enum ObjectType", "public static bool WriteObjectType(",
	} {
		if got := countAcross(csFiles, needle); got != 1 {
			t.Errorf("C#: %q emitted %d times across the unit — the compilation cannot succeed unless it is exactly once", needle, got)
		}
	}
	// both messages carry the dispatch property, beside their own class
	if got := countAcross(csFiles, "public override MessageType Type =>"); got != 2 {
		t.Errorf("C#: expected 2 Type dispatch overrides (one per message), got %d", got)
	}

	for _, mode := range []string{"union", "variant"} {
		cppFiles, err := cpp.Generate(u, cpp.Options{MessageRepr: mode})
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{
			// WriteMessage is the dispatch surface — deliberately plain `inline`,
			// outside the write-spine demand (see cpp emitWriteDispatchComment)
			"enum class MessageType", "inline bool WriteMessage(", "SCHEMA_READ_INLINE bool ReadMessage(",
			"enum class ObjectType", "SCHEMA_WRITE_INLINE bool WriteObjectType(", "SCHEMA_READ_INLINE bool ReadObjectType(",
		} {
			if got := countAcross(cppFiles, needle); got != 1 {
				t.Errorf("C++ (%s): %q emitted %d times across the unit — a TU including both headers cannot compile unless it is exactly once", mode, needle, got)
			}
		}
		// the owner file must include the other message file: the dispatch
		// value holds every message by value
		owner := ir.MessageOwner(u) + ".h"
		other := "MessagesA"
		if ir.MessageOwner(u) == "MessagesA" {
			other = "MessagesB"
		}
		if !strings.Contains(string(cppFiles[owner]), "#include \""+other+".h\"") {
			t.Errorf("C++ (%s): the dispatch owner %s does not include %s.h", mode, owner, other)
		}
	}
}
