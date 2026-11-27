package testinputs

import "testing"

// TestEveryInterpreterChildRegistersItsInputsPlants plants each form the
// guard must refuse and each it must accept, and checks the reader's verdict
// on every one.
func TestEveryInterpreterChildRegistersItsInputsPlants(t *testing.T) {
	t.Parallel()
	const head = "package p\nimport (\n\"context\"\n\"os/exec\"\n\"testing\"\n\"github.com/mas-bandwidth/schema/v3/internal/testproc\"\n\"github.com/mas-bandwidth/schema/v3/internal/testinputs\"\n)\nvar _ = context.Background\nvar _ = testinputs.Files\nvar _ = testproc.Command\n"
	cases := []struct {
		name, body string
		want       int
	}{
		{"sh through os/exec", "func TestA(t *testing.T) { exec.Command(\"sh\", \"make/x.sh\").Run() }", 1},
		{"bash through CommandContext", "func TestA(t *testing.T) { exec.CommandContext(context.Background(), \"bash\", \"x.sh\").Run() }", 1},
		{"make through testproc", "func TestA(t *testing.T) { testproc.Command(t, \"make\", \"-n\", \"x\").Run() }", 1},
		{"python3 through testproc CommandContext", "func TestA(t *testing.T) { testproc.CommandContext(t, context.Background(), \"python3\", \"tools/x.py\").Run() }", 1},
		{"node", "func TestA(t *testing.T) { exec.Command(\"node\", \"x.js\").Run() }", 1},
		{"wrapper running sh", "func TestA(t *testing.T, bin string) { exec.Command(bin, \"--\", \"sh\", \"x.sh\").Run() }", 1},
		{"registered in another function only", "func reg(t *testing.T) { testinputs.Files(t, \"x.sh\") }\nfunc TestA(t *testing.T) { exec.Command(\"sh\", \"x.sh\").Run() }", 1},
		{"directive with no reason", "func TestA(t *testing.T) {\n// testinputs:none\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		// gap 1: a call into the package that registers nothing
		{"Root only", "func TestA(t *testing.T) { _ = testinputs.Root(t); exec.Command(\"sh\", \"x.sh\").Run() }", 1},
		// gap 2: a directive whose reason is no reason
		{"directive with a dash and nothing after", "func TestA(t *testing.T) {\n// testinputs:none —\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		{"directive with punctuation only", "func TestA(t *testing.T) {\n// testinputs:none — ... !!\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		{"directive with one word", "func TestA(t *testing.T) {\n// testinputs:none — fine\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		{"directive with two words", "func TestA(t *testing.T) {\n// testinputs:none - no file\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		{"a longer name that starts with the directive", "func TestA(t *testing.T) {\n// testinputs:nonexistent reads no file here\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		{"Root and Files", "func TestA(t *testing.T) { _ = testinputs.Root(t); testinputs.Files(t, \"x.sh\"); exec.Command(\"sh\", \"x.sh\").Run() }", 0},
		{"registered through Build", "func TestA(t *testing.T) { testinputs.Build(t); exec.Command(\"make\", \"x\").Run() }", 0},
		{"directive with a hyphen and three words", "func TestA(t *testing.T) {\n// testinputs:none - reads no file\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 0},
		{"registered", "func TestA(t *testing.T) { testinputs.Files(t, \"make/x.sh\"); exec.Command(\"sh\", \"make/x.sh\").Run() }", 0},
		{"registered through Make", "func TestA(t *testing.T) { testinputs.Make(t); testproc.Command(t, \"make\", \"x\").Run() }", 0},
		{"directive with a reason", "func TestA(t *testing.T) {\n// testinputs:none — runs `exit 0`, reads no file\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 0},
		{"directive on the line", "func TestA(t *testing.T) {\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() // testinputs:none — reads no file\n}", 0},
		{"a compiled binary", "func TestA(t *testing.T, bin string) { exec.Command(bin, \"-x\").Run() }", 0},
		{"go, not an interpreter", "func TestA(t *testing.T) { exec.Command(\"go\", \"vet\").Run() }", 0},
	}
	refused := 0
	for _, c := range cases {
		bad, err := Check(c.name+"_test.go", []byte(head+c.body+"\n"))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(bad) != c.want {
			t.Errorf("%s: the guard refused %d start(s) %v, want %d", c.name, len(bad), bad, c.want)
		}
		if c.want > 0 {
			refused++
		}
	}
	if refused != 14 {
		t.Fatalf("planted %d refused forms, want 14: a guard that refuses nothing would pass", refused)
	}
	// a dot import of os/exec is resolved by binding, not spelling
	dot := "package p\nimport (\n. \"os/exec\"\n\"testing\"\n)\nfunc TestA(t *testing.T) { Command(\"sh\", \"x.sh\").Run() }\n"
	if bad, err := Check("dot_test.go", []byte(dot)); err != nil || len(bad) != 1 {
		t.Errorf("dot import: the guard refused %v (err %v), want 1", bad, err)
	}
}
