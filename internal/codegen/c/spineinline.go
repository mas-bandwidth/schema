package c

import "github.com/mas-bandwidth/schema/v2/ir"

// fileEmitsWire reports whether this wire file will emit any read/write wire
// function — struct or union wire pairs.
// file's message dispatch surface. Files that only carry consts/enums/flags
// emit no wire functions and no macro block.
func (g *gen) fileEmitsWire() bool {
	for _, d := range g.file.Decls {
		switch d.(type) {
		case *ir.Struct, *ir.Union:
			return true
		}
	}
	return false
}
