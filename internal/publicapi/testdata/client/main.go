// Command client is a schema compiler built OUTSIDE the schema module, on the
// public API alone: it registers a generator of its own — the thing the
// built-in six cannot demonstrate, since they might have had a private path
// into the driver — and reports the unit through the public IR.
//
// It is a module of its own (see go.mod), so Go's internal rule applies to it
// exactly as it applies to a stranger's repository: an import of
// github.com/mas-bandwidth/schema/internal/... does not compile here. What
// this program does is therefore what anyone can do.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/compiler"
	"github.com/mas-bandwidth/schema/ir"
)

// widths is a generator that emits nothing a language would compile — it
// reports each message's worst-case wire width, the bound the emitters
// advertise (SPEC §6.1 item 4). The point is the shape: a type outside this
// module satisfying compiler.Generator, reading the IR through the public
// derived-parameter helpers.
type widths struct{}

// Names registers this generator as `--lang widths`.
func (widths) Names() []string { return []string{"widths"} }

// Generate reports one line per message: worst-case bits, the write-buffer
// size that holds them, and the field count.
func (widths) Generate(u *ir.Unit, _ compiler.Options) (map[string][]byte, error) {
	var b strings.Builder
	for _, name := range u.Messages {
		st, ok := u.Structs[name]
		if !ok {
			return nil, fmt.Errorf("message %s has no struct in the unit", name)
		}
		bits := ir.MaxBitsStruct(st)
		fmt.Fprintf(&b, "%s %d bits %d bytes %d fields\n", name, bits, ir.MaxBytes(bits), len(st.Fields))
	}
	return map[string][]byte{"widths.txt": []byte(b.String())}, nil
}

func main() {
	c := compiler.New()
	if err := c.Register(widths{}); err != nil {
		die(err)
	}
	paths, err := compiler.GatherPaths(os.Args[1:])
	if err != nil {
		die(err)
	}
	// No FormatInPlace: a tool that only reads a tree should not rewrite it.
	u, err := c.Load(paths)
	if err != nil {
		die(err)
	}
	fmt.Printf("package %s 0x%016x\n", u.Package, u.ProtocolId)
	fmt.Printf("targets %s\n", strings.Join(c.Targets(), " "))
	files, err := c.Generate(u, "widths", nil)
	if err != nil {
		die(err)
	}
	if _, err := os.Stdout.Write(files["widths.txt"]); err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "client:", err)
	os.Exit(1)
}
