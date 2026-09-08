package ctable

import (
	"strings"
	"testing"
)

func TestSeparateCStatements(t *testing.T) {
	source := `#define STEPS(x) do { x++; x++; } while (0)
#define MORE(x) do { x++; \
 x++; } while (0)
/* a ; comment */
int f(int x) {
 const char * text="literal;still literal"; if(x)return 1;return 0;
 for(int i=0;i<3;i++)x++;x++;
 if(x) x++; else x--;
}
`
	got := separateCStatements(source)
	for _, want := range []string{`#define STEPS(x) do { x++; x++; } while (0)`, "#define MORE(x) do { x++; \\\n x++; } while (0)", `"literal;still literal"`, `for(int i=0;i<3;i++)`, "if(x)return 1;\n return 0;", "x++;\n else x--;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if separateCStatements(got) != got {
		t.Fatal("formatting is not idempotent")
	}
}
