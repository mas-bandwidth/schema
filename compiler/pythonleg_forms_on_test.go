package compiler

import "testing"

func init() { nonGoCarriers["python"] = append(nonGoCarriers["python"], carryForms) }

// This is the public-driver seam; the generated-code tests exercise each
// permitted and excluded runtime surface separately.
func TestPythonFormsPublicDriver(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		"package formprobe\ntable Root | forms=\"wire\" { x int32 }\n",
		"package formprobe\ntable Root | forms=\"wire,json\" { x int32 }\n",
		"package formprobe\ntable Root | forms=\"wire,block\" { x int32 }\n",
		"package formprobe\ntable Root | forms=\"wire,cook\" { x int32 }\n",
		"package formprobe\ntable Root | forms=\"wire,json,block,cook\" { x int32 }\n",
	} {
		if _, err := New().Generate(unitFromSource(t, schema), "python", nil); err != nil {
			t.Fatalf("Python refuses declared forms: %v", err)
		}
	}
}
