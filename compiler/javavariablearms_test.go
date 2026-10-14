package compiler

import (
	"slices"
	"testing"
)

// The public driver must admit the same constructs as the generated codecs
// (SPEC-TABLES §2.3, §2.6, §2.8); emitter-only tests cannot prove registration.
func TestJavaVariableCarrierRegistration(t *testing.T) {
	t.Parallel()
	for _, carrier := range []struct {
		name string
		set  []string
	}{{"maps", mapTargets}, {"optional arrays", optionalArrayTargets}, {"table union arms", tableArmTargets}, {"union arrays", unionArrayTargets}, {"both byte buffers", blobTargets}} {
		if !slices.Contains(carrier.set, "java") {
			t.Errorf("Java is not registered for %s", carrier.name)
		}
	}
}
