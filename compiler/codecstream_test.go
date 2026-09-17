// The --codec modes (SPEC §7): best is the default and every language's ruled
// fastest correct form; stream is the per-field hand-writer idiom the compiler
// emits so each platform's runtime stream path can be profiled with uniform,
// compiler-emitted code. The mode changes the calls, never the bits. This gate
// holds the two spellings, the target support, and the shape of each mode's
// output.
package compiler

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// codecSrc has one run of eight consecutive full-width scalars: the shape a
// flat word codec folds (Go, Rust, C#) and a runtime stream writes one call at
// a time.
const codecSrc = `package probe

type Shape
{
    a uint32
    b uint32
    c uint32
    d uint32
    e uint32
    f uint32
    g uint32
    h uint32
}
`

func TestCodecStreamMode(t *testing.T) {
	u := unitFromSource(t, codecSrc)
	c := New()

	t.Run("go", func(t *testing.T) {
		best := mustGenerate(t, c, u, "go", Options{})
		stream := mustGenerate(t, c, u, "go", Options{"codec": "stream"})

		bestGo := string(best["Probe.go"])
		if !strings.Contains(bestGo, "SerializeBits64") {
			t.Fatalf("the default Go codec must fold the run into word chunks; got:\n%s", bestGo)
		}
		if n := strings.Count(bestGo, "stream.SerializeBits64("); n == 0 {
			t.Fatalf("the default Go codec must emit SerializeBits64 chunks")
		}

		streamGo := string(stream["Probe.go"])
		// scope the count to the write function: the read twin makes the same
		// eight per-field calls, so the file carries sixteen.
		writeGo := streamGo
		if i := strings.Index(streamGo, "func ReadShape("); i >= 0 {
			writeGo = streamGo[:i]
		}
		if got := strings.Count(writeGo, "stream.SerializeBits("); got != 8 {
			t.Fatalf("the Go stream codec must emit one per-field call per scalar (want 8), got %d:\n%s", got, writeGo)
		}
		if strings.Contains(writeGo, "SerializeBits64") {
			t.Fatalf("the Go stream codec must not fold the run into word chunks:\n%s", writeGo)
		}
		for _, field := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
			want := "stream.SerializeBits(&value." + field + ", 32)"
			if !strings.Contains(writeGo, want) {
				t.Fatalf("the Go stream codec is missing %q:\n%s", want, writeGo)
			}
		}
	})

	t.Run("flat-runtimes-differ", func(t *testing.T) {
		for _, target := range []string{"rust", "cs"} {
			best := mustGenerate(t, c, u, target, Options{})
			stream := mustGenerate(t, c, u, target, Options{"codec": "stream"})
			if equalFiles(best, stream) {
				t.Fatalf("%s: --codec=stream must change the emitted calls", target)
			}
		}
	})

	t.Run("js-flat-tier", func(t *testing.T) {
		best := mustGenerate(t, c, u, "js", Options{})
		if _, ok := best["ProbeFlat.js"]; !ok {
			t.Fatalf("the default JS codec must emit the BaseFlat.js tier")
		}
		stream := mustGenerate(t, c, u, "js", Options{"codec": "stream"})
		if _, ok := stream["ProbeFlat.js"]; ok {
			t.Fatalf("--codec=stream must suppress the JS flat tier; the runtime tier is the stream codec")
		}
		if _, ok := stream["Probe.js"]; !ok {
			t.Fatalf("--codec=stream must still emit the JS runtime tier")
		}
	})

	t.Run("already-stream-targets-identical", func(t *testing.T) {
		// C and C++ best IS the per-field runtime stream idiom, so the mode is
		// honored by emitting the same bytes.
		for _, target := range []string{"c", "cpp"} {
			best := mustGenerate(t, c, u, target, Options{})
			stream := mustGenerate(t, c, u, target, Options{"codec": "stream"})
			if !equalFiles(best, stream) {
				t.Fatalf("%s: --codec=stream must be a no-op where best is already the stream idiom", target)
			}
		}
	})

	t.Run("self-contained-legs-refuse-by-name", func(t *testing.T) {
		for _, target := range []string{"java", "dart", "elixir"} {
			_, err := c.Generate(u, target, Options{"codec": "stream"})
			if err == nil {
				t.Fatalf("%s: --codec=stream must refuse while the target is self-contained", target)
			}
			if !strings.Contains(err.Error(), target) || !strings.Contains(err.Error(), "stream") {
				t.Fatalf("%s: refusal must name the target and the mode, got: %v", target, err)
			}
			if _, err := c.Generate(u, target, Options{}); err != nil {
				t.Fatalf("%s: the default codec must still generate: %v", target, err)
			}
		}
	})

	t.Run("unknown-codec-refused", func(t *testing.T) {
		_, err := c.Generate(u, "go", Options{"codec": "turbo"})
		if err == nil {
			t.Fatal("an unknown codec must be refused, never silently defaulted")
		}
		for _, want := range []string{"turbo", "best", "stream"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal must name %q; got: %v", want, err)
			}
		}
	})
}

func mustGenerate(t *testing.T, c *Compiler, u *ir.Unit, target string, opts Options) map[string][]byte {
	t.Helper()
	files, err := c.Generate(u, target, opts)
	if err != nil {
		t.Fatalf("%s (%v): %v", target, opts, err)
	}
	return files
}

func equalFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, data := range a {
		other, ok := b[name]
		if !ok || !bytes.Equal(data, other) {
			return false
		}
	}
	return true
}
