package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Configs are the preprocessor configurations a file is compared under:
//
//	0 the host compiler as it is (GCC, x86-64 Linux);
//	1 MSVC on 64-bit Windows: no GCC or Clang, no C11 atomics;
//	2 a compiler that is neither, with no C version and no byte order;
//	3 a big-endian AArch64 build with no C11 atomics;
//	4 the test hooks and NDEBUG, a C header read as C++ besides (its extern
//	  "C" blocks; a C++ file keeps its own __cplusplus), and every name the
//	  file's rewritten chains test that neither the compiler nor the file
//	  defines, defined (ProjectNames): another header's runtime guard, a
//	  build's knob such as FIXED_FORCE_BIG_ENDIAN;
//	5 Clang on Apple.
//
// They are not trusted to reach every branch: Compare names any branch of a
// rewritten chain none of them takes (coverage.go).
var Configs = [][]string{
	{},
	{"-U__GNUC__", "-U__clang__", "-U__x86_64__", "-D_MSC_VER=1930", "-D_WIN32=1", "-D_WIN64=1", "-D_M_X64=1", "-D__STDC_NO_ATOMICS__=1"},
	{"-U__GNUC__", "-U__clang__", "-U__x86_64__", "-U__STDC_VERSION__", "-U__BYTE_ORDER__"},
	{"-U__BYTE_ORDER__", "-D__BYTE_ORDER__=__ORDER_BIG_ENDIAN__", "-D__STDC_NO_ATOMICS__=1", "-D__aarch64__=1", "-U__x86_64__"},
	{"-DSCHEMA_FIXED_FLOOR_TEST_HOOKS", "-D__cplusplus=201703L", "-DNDEBUG"},
	{"-D__clang__=1", "-D__APPLE__=1", "-D__MACH__=1", "-U__linux__", "-U__linux", "-Ulinux", "-U__gnu_linux__", "-D__aarch64__=1", "-U__x86_64__"},
}

// projectConfig is the configuration ProjectNames extend.
const projectConfig = 4

var ppIdentifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
var ppDefineName = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*define[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)

// ProjectNames are the names the rewritten chains of src test that are
// neither reserved to the compiler (a leading double underscore, or an
// underscore and a capital) nor #defined anywhere in src: what a build or
// another header may define. Configuration 4 defines each of them.
func ProjectNames(src string, chains []markedChain) []string {
	own := map[string]bool{}
	for _, m := range ppDefineName.FindAllStringSubmatch(src, -1) {
		own[m[1]] = true
	}
	names := map[string]bool{}
	for _, c := range chains {
		for _, br := range c.branches {
			_, cond, _ := strings.Cut(br.directive, "#")
			for _, id := range ppIdentifier.FindAllString(cond, -1)[1:] {
				reserved := strings.HasPrefix(id, "__") || len(id) > 1 && id[0] == '_' && id[1] >= 'A' && id[1] <= 'Z'
				if id != "defined" && !reserved && !own[id] {
					names[id] = true
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// configsFor is Configs with configuration 4 extended by src's ProjectNames.
func configsFor(src string, chains []markedChain) [][]string {
	out := make([][]string, len(Configs))
	copy(out, Configs)
	extra := append([]string{}, Configs[projectConfig]...)
	for _, n := range ProjectNames(src, chains) {
		extra = append(extra, "-D"+n+"=1")
	}
	out[projectConfig] = extra
	return out
}

// an #include, #error or #warning becomes a plain line the preprocessor
// passes through where its branch is kept, so a rewrite that moves, drops or
// doubles one changes the tokens: no header outside the file is read (both
// forms are compared on their own text), and a guard that would stop the
// preprocessor for want of a header it did not read is compared, not run.
var ppIncludeLine = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*(include_next|include|error|warning)\b`)

func neutralizeIncludes(src string) string {
	return ppIncludeLine.ReplaceAllString(src, "schema_single_pass_$1")
}

// Preprocess runs the C or C++ preprocessor over src in directives-only mode
// under one configuration: conditionals are decided and #defines are kept as
// text, so what a branch defines is compared as well as what it declares.
// A preprocessor error is an error, never an empty stream.
func Preprocess(src, lang string, cfg []string) ([]string, error) {
	args := []string{"-E", "-P", "-fdirectives-only", "-x", lang}
	for _, a := range cfg {
		if lang == "c++" && strings.HasPrefix(a, "-D__cplusplus=") {
			continue
		}
		args = append(args, a)
	}
	args = append(args, "-")
	cmd := exec.Command(Compiler, args...)
	cmd.Stdin = strings.NewReader(neutralizeIncludes(src))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", Compiler, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return Tokens(out.String()), nil
}

// Compiler is the preprocessor the check runs (gcc's -fdirectives-only).
var Compiler = "gcc"

// Coverage counts what a Compare reached: the chains it took apart, their
// branches (each kept under some configuration), and the chains whose own
// context no configuration keeps (inside the second of two guarded twins, say), whose
// branches the preprocessor drops under every configuration.
type Coverage struct{ Chains, Branches, Unreached int }

// Compare preprocesses before and after under every configuration and returns
// the first difference, naming the configuration and the tokens around it;
// then every branch of every chain the rewrite took apart must have been kept
// under some configuration (markChains), or it is named as never compared.
func Compare(before, after, lang string) (Coverage, error) {
	marked, chains := markChains(before)
	var cov Coverage
	if len(chains) == 0 {
		return cov, fmt.Errorf("the rewrite changed a file in which no chain with an #elif or #else was found, so nothing in it is compared")
	}
	seen := map[string]bool{}
	configs := configsFor(before, chains)
	for i, cfg := range configs {
		tb, err := Preprocess(marked, lang, cfg)
		if err != nil {
			return cov, fmt.Errorf("config %d, the form as emitted: %w", i, err)
		}
		tb = takeMarkers(tb, seen)
		ta, err := Preprocess(after, lang, cfg)
		if err != nil {
			return cov, fmt.Errorf("config %d, the rewritten form: %w", i, err)
		}
		if len(tb) == 0 {
			return cov, fmt.Errorf("config %d: the preprocessor kept no token of the form as emitted", i)
		}
		at := 0
		for at < len(tb) && at < len(ta) && tb[at] == ta[at] {
			at++
		}
		if at == len(tb) && at == len(ta) {
			continue
		}
		lo := max(0, at-6)
		return cov, fmt.Errorf("config %d (%s): the tokens part at token %d of %d/%d: as emitted %q, rewritten %q",
			i, strings.Join(cfg, " "), at, len(tb), len(ta), tb[lo:min(at+6, len(tb))], ta[lo:min(at+6, len(ta))])
	}
	miss, cov := uncovered(chains, seen)
	if len(miss) > 0 {
		return cov, fmt.Errorf("%d branches of rewritten chains are kept under no configuration, so never compared: %s", len(miss), strings.Join(miss, "; "))
	}
	return cov, nil
}

// Tokens splits preprocessed C or C++ into tokens: whitespace and comments
// are dropped, string and character literals are kept whole, and every
// directive line the preprocessor kept ends in an explicit <nl> token.
func Tokens(src string) []string {
	var out []string
	i, n := 0, len(src)
	lineStart, inPP := true, false
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			if inPP {
				out = append(out, "<nl>")
				inPP = false
			}
			lineStart = true
			i++
			continue
		case c == '\\' && i+1 < n && src[i+1] == '\n':
			i += 2
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < n && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				i = n
			} else {
				i += j + 4
			}
			continue
		}
		if lineStart && c == '#' {
			inPP = true
		}
		lineStart = false
		switch {
		case c == '"' || c == '\'':
			j := i + 1
			for j < n && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			out = append(out, src[i:min(j+1, n)])
			i = j + 1
		case c == '_' || c == '.' || (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'z'):
			j := i
			for j < n {
				d := src[j]
				if d == '_' || d == '.' || (d >= '0' && d <= '9') || (d|0x20 >= 'a' && d|0x20 <= 'z') {
					j++
				} else {
					break
				}
			}
			out = append(out, src[i:j])
			i = j
		default:
			out = append(out, string(c))
			i++
		}
	}
	return out
}
