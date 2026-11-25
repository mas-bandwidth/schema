package ccommon

import (
	"fmt"
	"os"
)

// ConditionalsAsEmittedEnv names the switch that has the C and C++ table
// generators write each file as emitted, before SinglePassConditionals. The
// single-pass equivalence gate (make tables-single-pass-equivalence) sets it
// to "1" for one of its two generations and holds the rewrite to the same
// preprocessed tokens as what it rewrote; nothing else sets it. Any other
// value is refused, so a misspelt switch cannot quietly give either form.
const ConditionalsAsEmittedEnv = "SCHEMA_CONDITIONALS_AS_EMITTED"

// RewriteConditionals is SinglePassConditionals, unless the environment asks
// for the file as emitted (ConditionalsAsEmittedEnv).
func RewriteConditionals(src string) (string, error) {
	switch v := os.Getenv(ConditionalsAsEmittedEnv); v {
	case "":
		return SinglePassConditionals(src)
	case "1":
		return src, nil
	default:
		return "", fmt.Errorf("%s=%q: the switch is \"1\" or unset", ConditionalsAsEmittedEnv, v)
	}
}
