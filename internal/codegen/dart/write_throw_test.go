package dart

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// writeThrowSchema exercises every write path that used to unwind: a wstring
// (the two §4.12 writer rules) and a string(N) beside it.
const writeThrowSchema = "package t\n\n" +
	"type P\n{\n" +
	"    caption wstring(7)\n" +
	"    text    string(32)\n" +
	"    data    bytes(16)\n" +
	"}\n"

func generateSource(t *testing.T, src string) string {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	return all.String()
}

// writeBodies returns the text of every generated function whose name starts
// with "write", by brace counting from the signature line.
func writeBodies(out string) map[string]string {
	bodies := map[string]string{}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		idx := strings.Index(line, " write")
		if idx < 0 || !strings.Contains(line, "(") || !strings.HasSuffix(strings.TrimSpace(line), "{") {
			continue
		}
		name := line[idx+1:]
		if p := strings.Index(name, "("); p >= 0 {
			name = name[:p]
		}
		depth, body := 0, []string{}
		for _, l := range lines[i:] {
			body = append(body, l)
			depth += strings.Count(l, "{") - strings.Count(l, "}")
			if depth == 0 && len(body) > 1 {
				break
			}
		}
		bodies[name] = strings.Join(body, "\n")
	}
	return bodies
}

// TestWriteNeverThrows pins GLENN'S LAW and SPEC §5: write-side checks are
// DEBUG ONLY, and "no target panics and none throws: Elixir's raise is the only
// unwinding path in the nine". The Dart wstring write used to emit
// `throw ArgumentError('wstring length')` and `throw ArgumentError('wstring
// null unit')` — alive without --enable-asserts, where every other Dart write
// contract is an assert. This test is red on that emitter.
func TestWriteNeverThrows(t *testing.T) {
	out := generateSource(t, writeThrowSchema)
	bodies := writeBodies(out)
	if len(bodies) == 0 {
		t.Fatalf("no write functions found in the generated Dart")
	}
	for name, body := range bodies {
		if strings.Contains(body, "throw") {
			t.Errorf("%s: a write path throws — write contracts are assert only (SPEC §5):\n%s", name, body)
		}
	}
}

// TestWriteWStringContractsAreAsserts pins the replacement idiom and the
// clamp that keeps the release path from reaching past the N-unit buffer.
func TestWriteWStringContractsAreAsserts(t *testing.T) {
	out := generateSource(t, writeThrowSchema)
	for _, want := range []string{
		"assert(wideLength >= 0);",
		"assert(wideLength <= 7);",
		"assert(v != 0);",
		"final wideUsed = wideLength.clamp(0, 7);",
		"wideIndex < wideUsed;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated Dart is missing %q", want)
		}
	}
}
