package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pgoFixture lays out the smallest tree bench/run.sh will accept for the C and
// C++ legs: the driver itself, the shared provenance script, the runtime
// headers the §3.5 guard demands, and stub compilers/profilers on PATH that
// record the argv they were handed instead of building a real codec. The test
// asserts the TWO-PASS PGO recipe (#176), not a measurement.
func pgoFixture(t *testing.T) (root string, env []string) {
	t.Helper()
	root = t.TempDir()
	write := func(path, body string, mode os.FileMode) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	readFixture := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	write("bench/run.sh", readFixture("../run.sh"), 0o755)
	write("bench/tools/runtime-paths.sh", readFixture("runtime-paths.sh"), 0o755)
	write("serialize/serialize.h", "// stub serialize header\n", 0o644)
	write("serialize.c/serialize.c", "// stub serialize.c runtime\n", 0o644)
	write("generated/bench/cpp/BenchWire.h", "// stub generated c++\n", 0o644)
	write("generated/bench/c/BenchWire.h", "// stub generated c\n", 0o644)
	write("bench/cpp/bench_main.cpp", "// stub c++ runner\n", 0o644)
	write("bench/c/bench_main.c", "// stub c runner\n", 0o644)

	// The stub compiler records its argv, then writes an output "binary" that
	// emits one legal CSV row (and materializes the profraw the merge step
	// expects when the driver sets LLVM_PROFILE_FILE on the profile run).
	write("bin/fakecc", `#!/bin/bash
if [ "$1" = "--version" ]; then echo "clang version 18.0.0 (fake)"; exit 0; fi
echo "CC $*" >> "${PGO_TEST_LOG:?}"
out=""; prev=""
for a in "$@"; do
  [ "$prev" = "-o" ] && out="$a"
  prev="$a"
done
lang=cpp
case "$*" in *bench_main.c\ *|*bench_main.c) lang=c;; esac
cat > "$out" <<EOF
#!/bin/bash
if [ -n "\$LLVM_PROFILE_FILE" ]; then : > "\$LLVM_PROFILE_FILE"; fi
echo "$lang,bench_mixed,round_trip,1,10,1,1,1,1,1,0,0123456789abcdef,gen,hdr,contract,O3,unknown"
EOF
chmod +x "$out"
`, 0o755)
	write("bin/llvm-profdata", `#!/bin/bash
echo "PROFDATA $*" >> "${PGO_PROFILER_LOG:?}"
out=""
for a in "$@"; do case "$a" in -output=*) out="${a#-output=}";; esac; done
[ -n "$out" ] && : > "$out"
exit 0
`, 0o755)
	// taskset -c <cpu> <cmd...>: run the command on whatever CPU we already have.
	write("bin/taskset", "#!/bin/bash\nshift 2\nexec \"$@\"\n", 0o755)

	env = append(os.Environ(),
		"PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SERIALIZE="+filepath.Join(root, "serialize"),
		"SERIALIZE_C="+filepath.Join(root, "serialize.c"),
		"CC="+filepath.Join(root, "bin", "fakecc"),
		"BENCH_CPU=0",
		"PGO_TEST_LOG="+filepath.Join(root, "compiler.log"),
		"PGO_PROFILER_LOG="+filepath.Join(root, "profiler.log"),
	)
	return root, env
}

func pgoRun(t *testing.T, root string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{"bench/run.sh", "--compiler", filepath.Join(root, "bin", "fakecc")}, args...)...)
	cmd.Dir, cmd.Env = root, env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run.sh %v: %v\n%s", args, err, out)
	}
}

// TestPgoIsATwoPassNativeBuildForCAndCpp (#176): --pgo must compile the leg
// with -fprofile-instr-generate, run the instrumented binary once to write a
// profile, merge it with llvm-profdata, and rebuild with -fprofile-instr-use
// and -mcpu=native. Before the flag existed the driver refused it outright
// ("unknown argument: --pgo"), so this test is the red half of the card.
func TestPgoIsATwoPassNativeBuildForCAndCpp(t *testing.T) {
	root, env := pgoFixture(t)
	pgoRun(t, root, env, "--pgo", "--only", "cpp", "--out", filepath.Join(root, "cpp.csv"))
	pgoRun(t, root, env, "--pgo", "--only", "c", "--out", filepath.Join(root, "c.csv"))

	log, err := os.ReadFile(filepath.Join(root, "compiler.log"))
	if err != nil {
		t.Fatal(err)
	}
	prof, err := os.ReadFile(filepath.Join(root, "profiler.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prof), "merge") || !strings.Contains(string(prof), "profdata") {
		t.Fatalf("llvm-profdata merge was not run:\n%s", prof)
	}

	for _, leg := range []struct{ source, label string }{
		{"bench/cpp/bench_main.cpp", "cpp"},
		{"bench/c/bench_main.c", "c"},
	} {
		var gen, use int
		for _, line := range strings.Split(string(log), "\n") {
			if !strings.Contains(line, leg.source) {
				continue
			}
			if strings.Contains(line, "-fprofile-instr-generate") {
				gen++
			}
			if strings.Contains(line, "-fprofile-instr-use=") {
				if !strings.Contains(line, "-mcpu=native") {
					t.Fatalf("%s use-pass is not native-targeted:\n%s", leg.label, line)
				}
				use++
			}
		}
		if gen != 1 || use != 1 {
			t.Fatalf("%s builds: instrumented=%d use-pass=%d, want 1 each\n%s", leg.label, gen, use, log)
		}
	}

	for _, csv := range []string{"cpp.csv", "c.csv"} {
		body, err := os.ReadFile(filepath.Join(root, csv))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "# pgo:") {
			t.Fatalf("%s preamble does not record the PGO build:\n%s", csv, body)
		}
	}
}
