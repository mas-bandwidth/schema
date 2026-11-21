// A decimal literal never starts with a zero followed by another digit
// (SPEC §4.1). The compiler reads `010` as ten, while C, C++ and Go read the
// same spelling as octal eight and strict-mode JavaScript refuses it, so a
// leading zero would make the legs disagree on a size, a bound, a range, a
// constant or a default. The scanner refuses the spelling by name wherever a
// number can stand; `0` itself, hex, binary and a fraction after a lone zero
// stay lawful.
package scanner

import (
	"testing"
)

func TestDecimalLiteralWithLeadingZeroIsRefused(t *testing.T) {
	t.Parallel()
	refused := map[string]string{
		"const":   "package p\nconst X = 010\n",
		"size":    "package p\ntype P\n{\n    name string(010)\n}\n",
		"bound":   "package p\ntype P\n{\n    v [..017]uint8\n}\n",
		"range":   "package p\ntype P\n{\n    v int32 | min = 00, max = 100\n}\n",
		"default": "package p\ntable T\n{\n    v int32 = 07\n}\n",
		"expr":    "package p\nconst X = 1 + 09\n",
		"float":   "package p\nconst X = 01.5\n",
		"zeros":   "package p\nconst X = 00\n",
	}
	for name, src := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !hasSubstrs(errorTexts(src), "leading zero") {
				t.Fatalf("a decimal literal with a leading zero was not refused by name:\n%s\ngot %v", src, errorTexts(src))
			}
		})
	}
}

func TestDecimalLiteralWithoutLeadingZeroIsAccepted(t *testing.T) {
	t.Parallel()
	accepted := map[string]string{
		"zero":        "package p\nconst X = 0\n",
		"ten":         "package p\nconst X = 10\n",
		"hex":         "package p\nconst X = 0x0F\n",
		"binary":      "package p\nconst X = 0b0011\n",
		"fraction":    "package p\nconst X = 0.5\n",
		"exponent":    "package p\nconst X = 0e3\n",
		"range":       "package p\nconst X = 0\ntype P\n{\n    v [0..10]uint8\n}\n",
		"zero suffix": "package p\nconst X = 100\n",
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := errorTexts(src); len(errs) > 0 {
				t.Fatalf("a lawful literal was refused:\n%s\ngot %v", src, errs)
			}
		})
	}
}
