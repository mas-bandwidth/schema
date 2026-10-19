package javatable

import (
	"sync"
	"sync/atomic"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var (
	emitCheckerQual  atomic.Bool
	checkerQualUnits sync.Map
)

// SetCheckerQual enables or disables emitting Checker Framework @Signed and
// @Unsigned type-use qualifiers on primitive integer storage.
func SetCheckerQual(v bool) {
	emitCheckerQual.Store(v)
}

// GenerateWithCheckerQual emits the unit's Java table surface with Checker
// Framework @Signed/@Unsigned qualifiers enabled for u alone, without mutating
// the process-wide default.
func GenerateWithCheckerQual(u *ir.Unit) (map[string][]byte, error) {
	checkerQualUnits.Store(u, true)
	defer checkerQualUnits.Delete(u)
	return Generate(u)
}

func checkerQualEnabled(u *ir.Unit) bool {
	if emitCheckerQual.Load() {
		return true
	}
	if u != nil {
		if _, ok := checkerQualUnits.Load(u); ok {
			return true
		}
	}
	return false
}

// signednessAnnotation returns the Checker Framework type-use qualifier for
// primitive integer storage when enabled for u. Generated pair classes carry
// 128-bit values and are outside the checker's primitive signedness lattice.
func signednessAnnotation(u *ir.Unit, t ir.FieldType) string {
	if !checkerQualEnabled(u) || t.Pointer || t.Width > 64 {
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
