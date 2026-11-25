// The compile leg: generated code is fed to a REAL compiler. The structural
// oracle (oracle_test.go) catches what a regex can prove; this target hands
// the C and C++ output to clang -fsyntax-only, which proves the whole
// register at once — duplicate definitions, undeclared identifiers, include
// order, type errors — the classes that shipped precisely because no gate
// ever compiled what the fuzzer generated.
//
// Cost and where it runs: one clang invocation per language per iteration
// (~40-80ms each on this corpus), so the full campaign is a SLOWER, dedicated
// target — run it by hand or in a scheduled job:
//
//	go test ./internal/fuzz -run xxx -fuzz FuzzGeneratedCompiles -fuzztime 10m
//
// Under plain `go test` (the CI "fuzz seeds" step) only the seed corpus runs,
// which makes this file double as the corpus-seeded compile check: every
// schema in examples/ and examples128/ plus every hand seed has its generated
// C and C++ syntax-checked on every CI push, with zero configuration. That
// split — full compilation per iteration in a bounded slow target, seeds
// compiled always — is deliberate: per-iteration clang is ~1000x slower than
// the parse-check-generate path, so wiring it into FuzzPipeline would gut the
// throughput of the target that needs it most.
//
// Scope: C++ and C (clang -fsyntax-only), plus Go syntax via go/parser in the
// structural oracle on every FuzzPipeline iteration. Rust and C# are not
// compile-checked here: rustc needs the serialize crate graph built per
// iteration and C# needs a dotnet project, both of which belong to the make
// gate that already compiles the real corpus in all five languages.
package fuzz_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// toolchain discovers the compilers and the serialize runtime headers once.
// Anything missing skips the target rather than failing it: this leg is a
// deepening of coverage where the environment allows, not a new hard
// dependency for `go test ./...` on a bare machine.
type toolchain struct {
	cxx        string // C++ compiler driver
	cc         string // C compiler driver
	serialize  string // serialize (C++) checkout, for serialize.h
	serializeC string // serialize.c checkout, for its serialize.h
	testDir    string // repo test/ dir: hand headers behind cpp_native (vec_math.h)
	skip       string // non-empty: why the target cannot run here
}

var toolchainOnce = sync.OnceValue(func() *toolchain {
	tc := &toolchain{}
	for _, cand := range []string{"clang++", "c++"} {
		if p, err := exec.LookPath(cand); err == nil {
			tc.cxx = p
			break
		}
	}
	for _, cand := range []string{"clang", "cc"} {
		if p, err := exec.LookPath(cand); err == nil {
			tc.cc = p
			break
		}
	}
	if tc.cxx == "" || tc.cc == "" {
		tc.skip = "no C/C++ compiler on PATH"
		return tc
	}
	// The sibling layout README.md documents and CI clones: ../serialize and
	// ../serialize.c beside this repo. Package dir is internal/fuzz, so the
	// repo root is two levels up.
	root, err := filepath.Abs("../..")
	if err != nil {
		tc.skip = err.Error()
		return tc
	}
	tc.serialize = filepath.Join(filepath.Dir(root), "serialize")
	tc.serializeC = filepath.Join(filepath.Dir(root), "serialize.c")
	for _, h := range []string{
		filepath.Join(tc.serialize, "serialize.h"),
		filepath.Join(tc.serializeC, "serialize.h"),
	} {
		if _, err := os.Stat(h); err != nil {
			tc.skip = "serialize runtime headers not found at the documented sibling paths (" + h + ")"
			return tc
		}
	}
	tc.testDir = filepath.Join(root, "test")
	return tc
})

var quotedInclude = regexp.MustCompile(`(?m)^\s*#\s*include\s+"([^"]+)"`)

// externalIncludes returns the quoted includes in out that no generated file
// satisfies — the serialize runtime plus whatever hand headers cpp_native
// pulled in.
func externalIncludes(out map[string][]byte) map[string]bool {
	ext := map[string]bool{}
	for _, data := range out {
		for _, m := range quotedInclude.FindAllStringSubmatch(string(data), -1) {
			if _, generated := out[m[1]]; !generated {
				ext[m[1]] = true
			}
		}
	}
	return ext
}

// syntaxCheck writes one backend's output to dir and runs the compiler over a
// synthesized translation unit that includes every generated header — the
// consumer's-eye view, same as test/c/main.c takes. Returns compiler stderr
// on failure.
func syntaxCheck(t *testing.T, compiler string, flags []string, dir string, out map[string][]byte, ext string) (string, bool) {
	t.Helper()
	names := make([]string, 0, len(out))
	for name, data := range out {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o666); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names) // deterministic TU, deterministic diagnostics
	var tu strings.Builder
	for _, name := range names {
		tu.WriteString(`#include "` + name + "\"\n")
	}
	tuPath := filepath.Join(dir, ext)
	if err := os.WriteFile(tuPath, []byte(tu.String()), 0o666); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, compiler, append(append([]string{}, flags...), tuPath)...)
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		return string(stderr), false
	}
	return "", true
}

// FuzzGeneratedCompiles: any schema the checker accepts must generate C and
// C++ that clang accepts under the SAME strictness the Makefile imposes on
// the corpus (-Wall -Wextra -Werror: generated headers land in consumer
// translation units, so a warning here is a build break in their tree).
func FuzzGeneratedCompiles(f *testing.F) {
	tc := toolchainOnce()
	if tc.skip != "" {
		f.Skip("compile fuzz unavailable: " + tc.skip)
	}
	seedFromCorpus(f, func(s string) { f.Add(s) })
	for _, s := range handSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		unit := unitOf(map[string]string{"A.schema": src})
		if unit == nil {
			return
		}
		for _, b := range backends {
			if b.name != "cpp" && b.name != "c" {
				continue
			}
			out, err := b.generate(unit)
			if err != nil {
				continue
			}
			serialize, std := tc.serialize, "-std=c++17"
			compiler, tuName := tc.cxx, "tu.cpp"
			if b.name == "c" {
				serialize, std = tc.serializeC, "-std=c99"
				compiler, tuName = tc.cc, "tu.c"
			}
			dir := t.TempDir()
			flags := []string{
				"-fsyntax-only", std, "-Wall", "-Wextra", "-Werror", "-ffp-contract=off",
				"-I", dir, "-I", serialize,
			}
			// Hand headers named by cpp_native resolve against the repo's
			// test/ dir, exactly where make's corpus build finds them. A
			// fuzzed schema naming a header that exists nowhere is asking
			// for the CONSUMER's file, which is not a codegen defect: skip.
			needTestDir := false
			resolvable := true
			for inc := range externalIncludes(out) {
				if inc == "serialize.h" {
					continue
				}
				if _, err := os.Stat(filepath.Join(tc.testDir, inc)); err != nil {
					resolvable = false
					break
				}
				needTestDir = true
			}
			if !resolvable {
				continue
			}
			if needTestDir {
				flags = append(flags, "-I", tc.testDir)
			}
			if stderr, ok := syntaxCheck(t, compiler, flags, dir, out, tuName); !ok {
				srcs := make([]string, 0, len(out))
				for name := range out {
					srcs = append(srcs, name)
				}
				sort.Strings(srcs)
				var dump strings.Builder
				for _, name := range srcs {
					dump.WriteString("---- " + name + " ----\n")
					dump.Write(out[name])
				}
				t.Fatalf("%s: checker accepted a schema whose generated code does not compile.\n"+
					"---- schema ----\n%s\n---- %s ----\n%s\n%s",
					b.name, src, compiler, stderr, dump.String())
			}
		}
	})
}
