package javatable

import "github.com/mas-bandwidth/schema/v2/ir"

// signednessAnnotation returns the Checker Framework type-use qualifier for
// primitive integer storage. Generated pair classes carry 128-bit values and
// are outside the checker's primitive signedness lattice.
func signednessAnnotation(t ir.FieldType) string {
	if t.Pointer || t.Width > 64 {
		return ""
	}
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		if t.Signed {
			return "@org.checkerframework.checker.signedness.qual.Signed "
		}
		return "@org.checkerframework.checker.signedness.qual.Unsigned "
	case ir.TBits:
		return "@org.checkerframework.checker.signedness.qual.Unsigned "
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "@org.checkerframework.checker.signedness.qual.Unsigned "
		}
	}
	return ""
}
