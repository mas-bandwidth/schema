package ccommon

import (
	"strings"
	"testing"
)

// The equivalence gate reads the generator's output twice, once as emitted:
// the switch gives exactly the input back, unset gives the rewrite, and any
// other value is refused rather than read as either.
func TestRewriteConditionalsAsEmittedSwitch(t *testing.T) {
	const in = "#if defined( A )\nint a;\n#else\nint b;\n#endif\n"
	const rewritten = "#if defined( A )\nint a;\n#endif\n#if !( defined( A ) )\nint b;\n#endif\n"
	for _, tc := range []struct{ env, want, refusal string }{
		{"", rewritten, ""},
		{"1", in, ""},
		{"yes", "", `SCHEMA_CONDITIONALS_AS_EMITTED="yes"`},
		{"0", "", `SCHEMA_CONDITIONALS_AS_EMITTED="0"`},
	} {
		t.Run("env="+tc.env, func(t *testing.T) {
			t.Setenv(ConditionalsAsEmittedEnv, tc.env)
			got, err := RewriteConditionals(in)
			if tc.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("got %q, %v; want a refusal naming %s", got, err, tc.refusal)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
