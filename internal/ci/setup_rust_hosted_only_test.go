package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// checkEverySetupRustIsHostedOnly requires every dtolnay/rust-toolchain step in
// `.github/workflows/*.yml` to carry `runner.environment == 'github-hosted'`.
// `dtolnay/rust-toolchain` resolves floating upstream `stable` at run time; on
// self-hosted Linux and macOS fleet runners it overwrites `~/.rustup`'s pinned
// fleet compiler (`1.98.1`) with whatever Rust release shipped upstream that day.
func checkEverySetupRustIsHostedOnly(wfs map[string]string) ([]string, int) {
	var errs []string
	count := 0
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		lines := strings.Split(wfs[name], "\n")
		for i, raw := range lines {
			trimmed := strings.TrimSpace(raw)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			step := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if !strings.HasPrefix(step, "uses: dtolnay/rust-toolchain@") {
				continue
			}
			count++

			start := i
			for j := i; j >= 0; j-- {
				t := strings.TrimSpace(lines[j])
				if strings.HasPrefix(t, "#") {
					continue
				}
				indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
				if indent == 6 && strings.HasPrefix(t, "- ") {
					start = j
					break
				}
				if indent < 6 {
					break
				}
			}

			cond := ""
			for j := start; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if j > start {
					indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
					if strings.HasPrefix(t, "- ") && indent <= 6 {
						break
					}
					if indent < 6 && t != "" {
						break
					}
				}
				if rest, ok := strings.CutPrefix(t, "if:"); ok {
					cond = strings.TrimSpace(rest)
					break
				}
			}
			if !strings.Contains(cond, hostedRunnerCondition) {
				errs = append(errs, fmt.Sprintf(
					"%s:%d: dtolnay/rust-toolchain runs without `if: ... %s` (got %q): resolves floating upstream Rust on self-hosted fleet runners instead of using the pinned fleet toolchain",
					name, i+1, hostedRunnerCondition, cond,
				))
			}
		}
	}

	slices.Sort(errs)
	return errs, count
}

func loadAllWorkflows(t *testing.T, root string) map[string]string {
	t.Helper()
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("read workflows dir: %v", err)
	}
	wfs := map[string]string{}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yml") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(".github", "workflows", ent.Name()))
		b, err := os.ReadFile(filepath.Join(wfDir, ent.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		wfs[rel] = string(b)
	}
	return wfs
}

func TestEverySetupRustIsHostedOnly(t *testing.T) {
	t.Parallel()
	wfs := loadAllWorkflows(t, repoRoot(t))
	errs, count := checkEverySetupRustIsHostedOnly(wfs)
	if count < 10 {
		t.Fatalf("scanned only %d dtolnay/rust-toolchain steps across workflows; expected >= 10", count)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

func TestEverySetupRustIsHostedOnlyNegativeControls(t *testing.T) {
	t.Parallel()

	clean := map[string]string{
		".github/workflows/ok.yml": strings.Join([]string{
			"jobs:",
			"  leg:",
			"    steps:",
			"      - uses: dtolnay/rust-toolchain@6bed0761d98439e5a578e2877258200ad565ba87",
			"        if: matrix.rust != '' && runner.environment == 'github-hosted'",
			"        with:",
			"          components: clippy, rustfmt",
		}, "\n"),
	}
	if errs, count := checkEverySetupRustIsHostedOnly(clean); len(errs) != 0 || count != 1 {
		t.Fatalf("expected clean workflow to pass (count=1, errs=0), got count=%d errs=%v", count, errs)
	}

	for _, tc := range []struct {
		name string
		yml  string
	}{
		{
			name: "unguarded dtolnay/rust-toolchain step",
			yml: strings.Join([]string{
				"jobs:",
				"  release-gates:",
				"    steps:",
				"      - uses: dtolnay/rust-toolchain@6bed0761d98439e5a578e2877258200ad565ba87",
				"        with:",
				"          components: clippy, rustfmt",
			}, "\n"),
		},
		{
			name: "matrix.rust without github-hosted guard",
			yml: strings.Join([]string{
				"jobs:",
				"  conformance:",
				"    steps:",
				"      - uses: dtolnay/rust-toolchain@6bed0761d98439e5a578e2877258200ad565ba87",
				"        with:",
				"          components: clippy, rustfmt",
				"        if: matrix.rust != ''",
			}, "\n"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs, _ := checkEverySetupRustIsHostedOnly(map[string]string{".github/workflows/bad.yml": tc.yml})
			if len(errs) != 1 || !strings.Contains(errs[0], "dtolnay/rust-toolchain runs without") {
				t.Fatalf("planted %q did not fail as expected; got: %v", tc.name, errs)
			}
		})
	}
}

// TestRustBigEndianTargetChecksActiveRustcSysroot guards against a standalone
// fleet Rust SDK in RUSTUP_BIN (no rustup binary, no s390x libstd) falling
// through to an unrelated rustup binary later in PATH that reports
// s390x-unknown-linux-gnu installed for a different sysroot (macbook E0463).
func TestRustBigEndianTargetChecksActiveRustcSysroot(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	rustMkBytes, err := os.ReadFile(filepath.Join(root, "make", "rust.mk"))
	if err != nil {
		t.Fatal(err)
	}
	rustMk := string(rustMkBytes)
	_, after, ok := strings.Cut(rustMk, "tables-rust-big-endian: build/tables-generated-rust/.stamp\n")
	if !ok {
		t.Fatal("tables-rust-big-endian recipe not found in make/rust.mk")
	}
	recipe, _, ok := strings.Cut(after, "\n\n")
	if !ok {
		t.Fatal("end of tables-rust-big-endian recipe not found")
	}
	if strings.Contains(recipe, "rustup target list") {
		t.Fatalf("tables-rust-big-endian still consults `rustup target list` instead of the active rustc target-libdir:\n%s", recipe)
	}
	if !strings.Contains(recipe, "rustc --print target-libdir") {
		t.Fatalf("tables-rust-big-endian does not query `rustc --print target-libdir`:\n%s", recipe)
	}

	// Execute the guard logic directly against a mock standalone RUSTUP_BIN/rustc
	// (no s390x libstd) with a decoy rustup on PATH that claims s390x is installed.
	tmp := t.TempDir()
	sdkBin := filepath.Join(tmp, "sdk-bin")
	decoyBin := filepath.Join(tmp, "decoy-bin")
	sysrootEmpty := filepath.Join(tmp, "sysroot-empty")
	sysrootWithStd := filepath.Join(tmp, "sysroot-with-std")
	for _, d := range []string{sdkBin, decoyBin, sysrootEmpty, sysrootWithStd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sysrootWithStd, "libstd-deadbeef.rlib"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExec := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(filepath.Join(decoyBin, "rustup"), "#!/bin/sh\necho s390x-unknown-linux-gnu\n")
	writeExec(filepath.Join(sdkBin, "rustc"), "#!/bin/sh\necho \"$MOCK_TARGET_LIBDIR\"\n")
	writeExec(filepath.Join(sdkBin, "cargo"), "#!/bin/sh\necho CARGO_RAN\n")

	script := `
libdir="$(PATH="$RUSTUP_BIN:$PATH" rustc --print target-libdir --target s390x-unknown-linux-gnu 2>/dev/null || true)"
if [ -z "$libdir" ] || ! ls "$libdir"/libstd* >/dev/null 2>&1; then
  echo "SKIP tables-rust-big-endian: s390x-unknown-linux-gnu is not installed"
  exit 0
fi
PATH="$RUSTUP_BIN:$PATH" cargo check
`
	runProbe := func(targetLibdir string) string {
		cmd := exec.Command("sh", "-eu", "-c", script)
		cmd.Env = append(os.Environ(),
			"RUSTUP_BIN="+sdkBin,
			"PATH="+decoyBin+":/usr/bin:/bin",
			"MOCK_TARGET_LIBDIR="+targetLibdir,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("probe failed (%s): %v: %s", targetLibdir, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	if got := runProbe(sysrootEmpty); !strings.HasPrefix(got, "SKIP tables-rust-big-endian:") {
		t.Fatalf("missing libstd in active rustc sysroot did not skip: got %q", got)
	}
	if got := runProbe(sysrootWithStd); got != "CARGO_RAN" {
		t.Fatalf("installed libstd in active rustc sysroot did not run cargo: got %q", got)
	}
}

// TestGolangciLintCacheIsolatedPerWorkspace ensures multi-runner fleet hosts
// never share ~/.cache/golangci-lint's process lock ("Error: parallel
// golangci-lint is running") when golangci-lint-fast and lang-lint (go) or
// concurrent PRs run simultaneously on one machine.
func TestGolangciLintCacheIsolatedPerWorkspace(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "schema-go-generated-golangci-lint-gate.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(mkBytes)
	for lineNo, line := range strings.Split(mk, "\n") {
		if strings.Contains(line, "$(GOLANGCI_LINT) run") && !strings.Contains(line, "GOLANGCI_LINT_CACHE=") {
			t.Errorf("make/schema-go-generated-golangci-lint-gate.mk:%d runs $(GOLANGCI_LINT) without workspace-isolated GOLANGCI_LINT_CACHE: %s", lineNo+1, strings.TrimSpace(line))
		}
	}
}
