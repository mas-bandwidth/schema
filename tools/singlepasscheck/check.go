package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Configs are the preprocessor configurations a file is compared under: the
// host compiler as it is, then MSVC on ARM64, a compiler that is neither GCC
// nor MSVC with no C version or byte order, a big-endian AArch64 build with no
// C11 atomics, and the test hooks with NDEBUG (a C header read as C++ besides: its extern "C" blocks; a C++ file keeps its own __cplusplus). Between them every branch of
// the compiler, byte-order, atomics and inline-spelling chains the generators
// write is taken in at least one.
var Configs = [][]string{
	{},
	{"-U__GNUC__", "-U__clang__", "-D_MSC_VER=1930", "-D_M_ARM64=1", "-U__x86_64__"},
	{"-U__GNUC__", "-U__clang__", "-U__x86_64__", "-U__STDC_VERSION__", "-U__BYTE_ORDER__"},
	{"-U__BYTE_ORDER__", "-D__BYTE_ORDER__=__ORDER_BIG_ENDIAN__", "-DSCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN", "-D__STDC_NO_ATOMICS__=1", "-D__aarch64__=1", "-U__x86_64__"},
	{"-DSCHEMA_FIXED_FLOOR_TEST_HOOKS", "-D__cplusplus=201703L", "-DNDEBUG"},
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

// Compare preprocesses before and after under every configuration and returns
// the first difference, naming the configuration and the tokens around it.
func Compare(before, after, lang string) error {
	for i, cfg := range Configs {
		tb, err := Preprocess(before, lang, cfg)
		if err != nil {
			return fmt.Errorf("config %d, the form as emitted: %w", i, err)
		}
		ta, err := Preprocess(after, lang, cfg)
		if err != nil {
			return fmt.Errorf("config %d, the rewritten form: %w", i, err)
		}
		if len(tb) == 0 {
			return fmt.Errorf("config %d: the preprocessor kept no token of the form as emitted", i)
		}
		at := 0
		for at < len(tb) && at < len(ta) && tb[at] == ta[at] {
			at++
		}
		if at == len(tb) && at == len(ta) {
			continue
		}
		lo := max(0, at-6)
		return fmt.Errorf("config %d (%s): the tokens part at token %d of %d/%d: as emitted %q, rewritten %q",
			i, strings.Join(cfg, " "), at, len(tb), len(ta), tb[lo:min(at+6, len(tb))], ta[lo:min(at+6, len(ta))])
	}
	return nil
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
