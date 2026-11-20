package ci

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// Matches bare GNU `timeout <duration> ...` in command position (after
	// start-of-line, shell control keywords, subshells, pipes/chains, or
	// leading VAR=val assignments), while never matching `--timeout` or
	// `-timeout` flags.
	bareTimeoutCmdRe = regexp.MustCompile(
		`(?:^|[;&|({]\s*|\b(?:if|then|else|elif|do)\s+|!\s+)` +
			`[@+-]*` +
			`(?:[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|[^\s;&|]+)\s+)*` +
			`(?:env\s+)?timeout\s+\S`,
	)

	makeTargetHeaderRe = regexp.MustCompile(`^([^\s:#=][^:=]*?)\s*:(?:[^=]|$)`)
	makeDefineHeaderRe = regexp.MustCompile(`^define\s+([A-Za-z0-9_.-]+)`)

	harnessRunRe = regexp.MustCompile(
		`(?:conformance-harness(?:-vm)?|\$\([A-Za-z0-9_]*HARNESS[A-Za-z0-9_]*\))\s+run\b([^;&|>\n]*)`,
	)
	workFlagRe = regexp.MustCompile(`(?:^|\s)--work(?:=|\s+)(?:"([^"]+)"|'([^']+)'|([^\s"'\x60;&|>]+))`)
)

type makeLogicalLine struct {
	lineNum  int
	rawFirst string
	joined   string
}

func joinContinuations(content string) []makeLogicalLine {
	rawLines := strings.Split(content, "\n")
	out := make([]makeLogicalLine, 0, len(rawLines))
	for i := 0; i < len(rawLines); {
		start := i + 1
		first := rawLines[i]
		var b strings.Builder
		b.WriteString(strings.TrimRight(first, "\r"))
		for strings.HasSuffix(b.String(), `\`) && i+1 < len(rawLines) {
			cur := b.String()
			b.Reset()
			b.WriteString(cur[:len(cur)-1])
			b.WriteByte(' ')
			i++
			b.WriteString(strings.TrimSpace(strings.TrimRight(rawLines[i], "\r")))
		}
		i++
		out = append(out, makeLogicalLine{
			lineNum:  start,
			rawFirst: first,
			joined:   b.String(),
		})
	}
	return out
}

// stripUnquotedComment removes a `# ...` comment when `#` occurs outside single
// or double quotes and after start-of-line or whitespace.
func stripUnquotedComment(s string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t' || s[i-1] == ';') {
			return s[:i]
		}
	}
	return s
}

type harnessWorkSite struct {
	file  string
	line  int
	scope string
	work  string
}

func checkMakefileAndScriptPortability(makeFiles map[string]string, shFiles map[string]string) ([]string, int) {
	var errs []string
	var workSites []harnessWorkSite

	var makeNames []string
	for name := range makeFiles {
		makeNames = append(makeNames, name)
	}
	sort.Strings(makeNames)

	for _, name := range makeNames {
		lines := joinContinuations(makeFiles[name])
		scope := "<top>"
		inDefine := false
		scopeHasWildcards := false
		scopeWildcardsLine := 0
		scopeHasBsdtar := false

		flushTarScope := func() {
			if scopeHasWildcards && !scopeHasBsdtar {
				errs = append(errs, fmt.Sprintf(
					"%s:%d (%s): uses GNU-only `tar --wildcards` without a `bsdtar` fallback branch",
					name, scopeWildcardsLine, scope,
				))
			}
			scopeHasWildcards = false
			scopeWildcardsLine = 0
			scopeHasBsdtar = false
		}

		for _, ll := range lines {
			trimmed := strings.TrimSpace(ll.joined)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if trimmed == "endef" {
				flushTarScope()
				inDefine = false
				scope = "<top>"
				continue
			}
			if m := makeDefineHeaderRe.FindStringSubmatch(trimmed); m != nil {
				flushTarScope()
				inDefine = true
				scope = "define:" + m[1]
				continue
			}
			isRecipe := strings.HasPrefix(ll.rawFirst, "\t") || inDefine
			if !isRecipe {
				if m := makeTargetHeaderRe.FindStringSubmatch(ll.joined); m != nil {
					flushTarScope()
					scope = strings.TrimSpace(m[1])
				}
				continue
			}

			code := strings.TrimSpace(stripUnquotedComment(ll.joined))
			if code == "" {
				continue
			}

			if bareTimeoutCmdRe.MatchString(code) {
				errs = append(errs, fmt.Sprintf(
					"%s:%d (%s): invokes bare GNU `timeout` CLI (fails on macOS runners without coreutils; use a tool flag such as `--timeout` or Go context deadlines): %s",
					name, ll.lineNum, scope, code,
				))
			}

			if strings.Contains(code, "bsdtar") {
				scopeHasBsdtar = true
			}
			if strings.Contains(code, "tar") && strings.Contains(code, "--wildcards") {
				scopeHasWildcards = true
				if scopeWildcardsLine == 0 {
					scopeWildcardsLine = ll.lineNum
				}
			}

			for _, hm := range harnessRunRe.FindAllStringSubmatch(code, -1) {
				args := hm[1]
				wm := workFlagRe.FindStringSubmatch(args)
				work := ""
				if wm != nil {
					for _, g := range wm[1:] {
						if g != "" {
							work = strings.TrimRight(g, "/")
							break
						}
					}
				}
				workSites = append(workSites, harnessWorkSite{
					file:  name,
					line:  ll.lineNum,
					scope: scope,
					work:  work,
				})
			}
		}
		flushTarScope()
	}

	// Validate `--work` invariants across all conformance-harness run invocations.
	byConcreteWork := map[string][]harnessWorkSite{}
	for _, s := range workSites {
		if s.work == "" {
			errs = append(errs, fmt.Sprintf(
				"%s:%d (%s): `conformance-harness run` omits `--work` (falls back to shared default `build/conformance` and races under `make -j`)",
				s.file, s.line, s.scope,
			))
			continue
		}
		if s.work == "build/conformance/fixtures" || strings.HasSuffix(s.work, "/conformance/fixtures") {
			errs = append(errs, fmt.Sprintf(
				"%s:%d (%s): `conformance-harness run` specifies shared fixtures path `--work %s`",
				s.file, s.line, s.scope, s.work,
			))
			continue
		}
		// Macro-parameterized paths ($(1), $$dir, $@, etc.) expand per target or
		// invocation; concrete paths must belong to a single target.
		if strings.Contains(s.work, "$(1)") || strings.Contains(s.work, "$$") || strings.Contains(s.work, "$@") {
			continue
		}
		byConcreteWork[s.work] = append(byConcreteWork[s.work], s)
	}

	var concretePaths []string
	for w := range byConcreteWork {
		concretePaths = append(concretePaths, w)
	}
	sort.Strings(concretePaths)
	for _, w := range concretePaths {
		sites := byConcreteWork[w]
		distinctTargets := map[string]harnessWorkSite{}
		for _, s := range sites {
			key := s.file + "::" + s.scope
			if _, exists := distinctTargets[key]; !exists {
				distinctTargets[key] = s
			}
		}
		if len(distinctTargets) > 1 {
			var parts []string
			for _, s := range sites {
				parts = append(parts, fmt.Sprintf("%s:%d (%s)", s.file, s.line, s.scope))
			}
			sort.Strings(parts)
			errs = append(errs, fmt.Sprintf(
				"multiple Make targets share `--work %s` (races under `make -j`): %s",
				w, strings.Join(parts, ", "),
			))
		}
	}

	// Shell scripts (`tools/**/*.sh`, `make/**/*.sh`).
	var shNames []string
	for name := range shFiles {
		shNames = append(shNames, name)
	}
	sort.Strings(shNames)

	for _, name := range shNames {
		content := shFiles[name]
		hasBsdtar := strings.Contains(content, "bsdtar")
		for _, ll := range joinContinuations(content) {
			code := strings.TrimSpace(stripUnquotedComment(ll.joined))
			if code == "" {
				continue
			}
			if bareTimeoutCmdRe.MatchString(code) {
				errs = append(errs, fmt.Sprintf(
					"%s:%d: invokes bare GNU `timeout` CLI (fails on macOS runners without coreutils): %s",
					name, ll.lineNum, code,
				))
			}
			if strings.Contains(code, "tar") && strings.Contains(code, "--wildcards") && !hasBsdtar {
				errs = append(errs, fmt.Sprintf(
					"%s:%d: uses GNU-only `tar --wildcards` without a `bsdtar` fallback branch",
					name, ll.lineNum,
				))
			}
		}
	}

	sort.Strings(errs)
	return errs, len(workSites)
}

func loadRepoPortabilityInputs(t *testing.T, root string) (map[string]string, map[string]string) {
	t.Helper()
	makeFiles := map[string]string{}
	shFiles := map[string]string{}

	mfBytes, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makeFiles["Makefile"] = string(mfBytes)

	for _, dir := range []string{"make", "tools"} {
		absDir := filepath.Join(root, dir)
		err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			switch filepath.Ext(path) {
			case ".mk":
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				makeFiles[rel] = string(b)
			case ".sh":
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				shFiles[rel] = string(b)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return makeFiles, shFiles
}

func TestMakefilePortability(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	makeFiles, shFiles := loadRepoPortabilityInputs(t, root)
	if len(makeFiles) < 20 {
		t.Fatalf("loaded only %d Make files; expected Makefile + make/*.mk", len(makeFiles))
	}
	if len(shFiles) < 10 {
		t.Fatalf("loaded only %d shell scripts under make/ and tools/", len(shFiles))
	}

	errs, runSites := checkMakefileAndScriptPortability(makeFiles, shFiles)
	if runSites < 100 {
		t.Fatalf("scanned only %d conformance-harness run invocations; expected >= 100", runSites)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

func TestMakefilePortabilityNegativeControls(t *testing.T) {
	t.Parallel()

	// Positive baseline: `--timeout`, `-timeout`, `tar --wildcards` with `bsdtar`
	// fallback, and distinct `--work` paths all pass cleanly.
	cleanMake := map[string]string{
		"Makefile": strings.Join([]string{
			"target-a:",
			"\t@./build/conformance-harness wire-fuzz --timeout 30s --seed 1 --n 10",
			"\tgo test -timeout 60s ./...",
			"\t@if command -v bsdtar >/dev/null 2>&1; then bsdtar -xf a.tar '*.so'; else tar --wildcards -xf a.tar '*.so'; fi",
			"\t./build/conformance-harness run --only cpp --work build/work-a",
			"",
			"target-b:",
			"\t./build/conformance-harness-vm run --only js --work build/work-b",
		}, "\n"),
	}
	cleanSh := map[string]string{
		"tools/ok.sh": "#!/bin/sh\n# timeout in comment is fine\nif command -v bsdtar >/dev/null 2>&1; then bsdtar -xf x.tar '*.a'; else tar --wildcards -xf x.tar '*.a'; fi\n./run --timeout 10s\n",
	}
	if errs, sites := checkMakefileAndScriptPortability(cleanMake, cleanSh); len(errs) != 0 || sites != 2 {
		t.Fatalf("expected clean baseline (2 sites, 0 errs), got sites=%d errs=%v", sites, errs)
	}

	cases := []struct {
		name      string
		makeFiles map[string]string
		shFiles   map[string]string
		wantSub   string
	}{
		{
			name: "bare timeout in make recipe",
			makeFiles: map[string]string{
				"make/bad.mk": "fuzz-target:\n\t@timeout 30s ./build/conformance-harness wire-fuzz\n",
			},
			wantSub: "invokes bare GNU `timeout` CLI",
		},
		{
			name: "env-prefixed bare timeout in make if-guard",
			makeFiles: map[string]string{
				"make/bad.mk": "fuzz-nc:\n\t@if CARDS_DROP=1 timeout 20s ./build/conformance-harness wire-fuzz; then exit 1; fi\n",
			},
			wantSub: "invokes bare GNU `timeout` CLI",
		},
		{
			name: "bare timeout in tools shell script",
			shFiles: map[string]string{
				"tools/bad.sh": "#!/bin/sh\ntimeout 15 python3 check.py\n",
			},
			wantSub: "tools/bad.sh:2: invokes bare GNU `timeout` CLI",
		},
		{
			name: "tar --wildcards without bsdtar in make recipe",
			makeFiles: map[string]string{
				"make/bad.mk": "dist-tool:\n\ttar -xzf dist/tool.tar.gz --wildcards '*/bin/tool'\n",
			},
			wantSub: "uses GNU-only `tar --wildcards` without a `bsdtar` fallback",
		},
		{
			name: "tar --wildcards without bsdtar in shell script",
			shFiles: map[string]string{
				"tools/unpack.sh": "#!/bin/sh\ntar --wildcards -xf pkg.tar '*.bin'\n",
			},
			wantSub: "tools/unpack.sh:2: uses GNU-only `tar --wildcards` without a `bsdtar` fallback",
		},
		{
			name: "conformance-harness run missing --work",
			makeFiles: map[string]string{
				"Makefile": "bad-conf:\n\t./build/conformance-harness run --only cpp\n",
			},
			wantSub: "omits `--work`",
		},
		{
			name: "conformance-harness run with bare build/conformance/fixtures",
			makeFiles: map[string]string{
				"make/bad.mk": "bad-fixtures:\n\t./build/conformance-harness run --drivers a.txt --work build/conformance/fixtures\n",
			},
			wantSub: "specifies shared fixtures path `--work build/conformance/fixtures`",
		},
		{
			name: "two distinct targets sharing concrete --work path",
			makeFiles: map[string]string{
				"make/a.mk": "target-one:\n\t./build/conformance-harness run --only c --work build/shared-c-work\n",
				"make/b.mk": "target-two:\n\t./build/conformance-harness run --only c --work build/shared-c-work\n",
			},
			wantSub: "multiple Make targets share `--work build/shared-c-work`",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs, _ := checkMakefileAndScriptPortability(tc.makeFiles, tc.shFiles)
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.wantSub) {
				t.Fatalf("planted case %q did not report %q; got:\n%s", tc.name, tc.wantSub, joined)
			}
		})
	}
}
