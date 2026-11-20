package ci

import (
	"fmt"
	"os"
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
