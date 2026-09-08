package compiler

import (
	"strings"
	"testing"
)

// Issue #714: Table JSON Base64 writer wraps signed 32-bit integer at INT32_MAX.
//
// In cpptable, ctable, cstable, and javatable, the Base64 writer loop previously tested:
//   for ( ; i + 3 <= length; i += 3 )
// When length == 2147483647 (MaxInt32, which is legally permitted by the compiler check
// for bytes(N) and *bytes), evaluating i + 3 when i == 2147483646 overflows signed 32-bit
// integer to -2147483647. Because -2147483647 <= 2147483647 evaluates to true, the loop
// continues past the end of the buffer, causing out-of-bounds reads and infinite looping.
//
// The loop condition must be structured as remaining-length subtraction:
//   cpp / c / java: length - i >= 3
//   cs:             data.Length - i >= 3
// Because i <= length is guaranteed, length - i never underflows or overflows,
// eliminating the integer wrap completely without requiring 64-bit promotion.
func TestIssue714Base64WriterIntegerWrap(t *testing.T) {
	u := unitFromSource(t, "package p\ntable Blob { payload bytes(16) }\n")
	c := New()

	tests := []struct {
		lang      string
		forbidden string
		required  string
	}{
		{"cpp", "i + 3 <= length", "length - i >= 3"},
		{"c", "i + 3 <= length", "length - i >= 3"},
		{"cs", "i + 3 <= data.Length", "data.Length - i >= 3"},
		{"java", "i + 3 <= length", "length - i >= 3"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			files, err := c.Generate(u, tt.lang, Options{})
			if err != nil {
				t.Fatalf("generate %s failed: %v", tt.lang, err)
			}

			var foundBase64 bool
			for _, content := range files {
				str := string(content)
				if strings.Contains(str, "Base64") || strings.Contains(str, "base64") {
					foundBase64 = true
					if strings.Contains(str, tt.forbidden) {
						t.Errorf("%s: emitted code contains wrapping loop condition %q", tt.lang, tt.forbidden)
					}
					if !strings.Contains(str, tt.required) {
						t.Errorf("%s: emitted code missing safe loop condition %q", tt.lang, tt.required)
					}
				}
			}

			if !foundBase64 {
				t.Fatalf("%s: generated code did not contain Base64 writer", tt.lang)
			}
		})
	}
}
