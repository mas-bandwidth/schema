package compiler

import (
	"strings"
	"testing"
)

func TestPythonKeyedEnumThroughDriver(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`package enumkey
        enum Slot { head, body, feet }
        enum Grade { Low, Mid, Old | deprecated }
        fixed table Root { ranks [Slot]Grade }`,
		`package enumkey
        enum Slot { head, body, feet }
        enum Grade { Low, Mid, Old | deprecated }
        type Scores { ranks [Slot]Grade }
        fixed table Holder { values Scores }`,
	} {
		c := New()
		u, err := c.Load(writeSchemas(t, map[string]string{"Core.schema": src}))
		if err != nil {
			t.Fatal(err)
		}
		files, err := c.Generate(u, "python", nil)
		if err != nil {
			t.Fatalf("driver refused keyed enum: %v", err)
		}
		code := string(files["enumkey/Core.py"])
		if !strings.Contains(code, `codec="keyed"`) || !strings.Contains(code, `codec="enum"`) || !strings.Contains(code, `variant_ids=`) {
			t.Fatal("driver did not emit keyed enum metadata")
		}
	}
}
