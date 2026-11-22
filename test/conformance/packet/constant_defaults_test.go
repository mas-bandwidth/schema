package packet

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Each writer constructs the packet without assigning its fields. The pins
// are independent little-endian int64 encodings of (-1, 0) and 4294967296.
func TestPacketConstantDefaultWriters(t *testing.T) {
	slowtest.Gate(t, "the C and C++ compilers and serialize runtimes")
	t.Chdir("../../..")
	for _, name := range []string{"unsigned-subtraction", "hexadecimal-addition"} {
		t.Run(name, func(t *testing.T) {
			c := compiler.New()
			u, err := c.Load([]string{filepath.Join("test/conformance/packet/testdata/constant-defaults", name, "Defaults.schema")})
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata/wire/packet-defaults", name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			for _, lang := range []string{"c", "cpp"} {
				t.Run(lang, func(t *testing.T) {
					files, err := c.Generate(u, lang, compiler.Options{})
					if err != nil {
						t.Fatal(err)
					}
					dir := t.TempDir()
					src := "defaults." + lang
					files[src] = driverSource(t, lang, src)
					writeTree(t, dir, files)
					if lang == "c" {
						rt := sibling("SERIALIZE_C", "serialize.c", "serialize.c")
						if rt == "" {
							t.Fatal("stage serialize.c or set SERIALIZE_C")
						}
						run(t, dir, nil, envOr("CC", "cc"), "-std=c99", "-I"+rt, src, filepath.Join(rt, "serialize.c"), "-lm", "-o", "writer")
					} else {
						rt := sibling("SERIALIZE", "serialize", "serialize.h")
						if rt == "" {
							t.Fatal("stage serialize or set SERIALIZE")
						}
						run(t, dir, nil, envOr("CXX", "c++"), "-std=c++17", "-I"+rt, src, "-o", "writer")
					}
					got := run(t, dir, nil, filepath.Join(dir, "writer"))
					if !bytes.Equal(got, want) {
						t.Errorf("fresh packet wrote %x, pinned bytes %x", got, want)
					}
				})
			}
		})
	}
}
