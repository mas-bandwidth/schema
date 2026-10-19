package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCSCookHashArithmeticUnchecked guards the C# cook test harness against
// unwrapped modulo-2^64 hash and PRNG arithmetic while keeping layout, offset,
// size, alignment, and bounds arithmetic checked under /checked+.
func TestCSCookHashArithmeticUnchecked(t *testing.T) {
	root := repoRoot(t)

	csprojBytes, err := os.ReadFile(filepath.Join(root, "test", "cs-cook", "schemacook.csproj"))
	if err != nil {
		t.Fatalf("read schemacook.csproj: %v", err)
	}
	if !strings.Contains(string(csprojBytes), "<CheckForOverflowUnderflow") || !strings.Contains(string(csprojBytes), ">true</CheckForOverflowUnderflow>") {
		t.Fatalf("test/cs-cook/schemacook.csproj must enable CheckForOverflowUnderflow=true")
	}

	walkBytes, err := os.ReadFile(filepath.Join(root, "test", "cs-cook", "src", "Walk.cs"))
	if err != nil {
		t.Fatalf("read Walk.cs: %v", err)
	}
	walkSrc := string(walkBytes)

	programBytes, err := os.ReadFile(filepath.Join(root, "test", "cs-cook", "src", "Program.cs"))
	if err != nil {
		t.Fatalf("read Program.cs: %v", err)
	}
	programSrc := string(programBytes)

	nativeBytes, err := os.ReadFile(filepath.Join(root, "test", "cs-cook", "src", "Native.cs"))
	if err != nil {
		t.Fatalf("read Native.cs: %v", err)
	}
	nativeSrc := string(nativeBytes)

	// 1. Header.Fnv1a64 in Walk.cs must wrap the FNV-1a prime multiply in unchecked
	// with a modulo 2^64 comment, and nothing else in Walk.cs or Native.cs may use unchecked.
	fnvBlock := extractBraceBlock(t, walkSrc, "public static ulong Fnv1a64(string s)")
	if !strings.Contains(fnvBlock, "unchecked") || !strings.Contains(fnvBlock, "0x100000001b3UL") {
		t.Fatalf("Header.Fnv1a64 in Walk.cs must wrap 0x100000001b3UL multiply in unchecked:\n%s", fnvBlock)
	}
	if !strings.Contains(fnvBlock, "modulo 2^64") {
		t.Fatalf("Header.Fnv1a64 in Walk.cs must carry a modulo 2^64 comment:\n%s", fnvBlock)
	}
	if strings.Count(walkSrc, "unchecked") != 1 {
		t.Fatalf("Walk.cs must have exactly 1 unchecked block (Header.Fnv1a64), got %d", strings.Count(walkSrc, "unchecked"))
	}
	if strings.Contains(nativeSrc, "unchecked") {
		t.Fatalf("Native.cs bounds and mmap placement arithmetic must remain checked (found unchecked)")
	}

	// 2. Checked layout/offset/bounds routines in Walk.cs must remain free of unchecked.
	for _, sig := range []string{
		"static void Storage(",
		"public static ulong Read64(byte[] b, int at)",
		"public static ulong DataOffset(ulong alignment)",
		"public ulong Offset(ulong i)",
		"public ulong Type(ulong i)",
		"public static Directory Of(byte* file, ulong length)",
	} {
		block := extractBraceBlock(t, walkSrc, sig)
		if strings.Contains(block, "unchecked") {
			t.Fatalf("routine %q in Walk.cs must remain checked, found unchecked:\n%s", sig, block)
		}
	}

	// 3. Rng.Next and Program.Mix in Program.cs must wrap SplitMix64 arithmetic in
	// unchecked with modulo 2^64 comments.
	for _, sig := range []string{
		"public ulong Next()",
		"static ulong Mix(ulong a, ulong b)",
	} {
		block := extractBraceBlock(t, programSrc, sig)
		if !strings.Contains(block, "unchecked") {
			t.Fatalf("routine %q in Program.cs must wrap SplitMix64 arithmetic in unchecked:\n%s", sig, block)
		}
		if !strings.Contains(block, "modulo 2^64") {
			t.Fatalf("routine %q in Program.cs must carry a modulo 2^64 comment:\n%s", sig, block)
		}
		for _, c := range []string{"0x9e3779b97f4a7c15UL", "0xbf58476d1ce4e5b9UL", "0x94d049bb133111ebUL"} {
			if !strings.Contains(block, c) {
				t.Fatalf("routine %q in Program.cs missing SplitMix64 constant %s", sig, c)
			}
		}
	}

	// 4. Every occurrence of FNV-1a or SplitMix64 constants across test/cs-cook/src/*.cs
	// must lie inside an unchecked block.
	hashConsts := regexp.MustCompile(`0x(?:100000001b3|9e3779b97f4a7c15|bf58476d1ce4e5b9|94d049bb133111eb)UL`)
	for name, src := range map[string]string{
		"Walk.cs":    walkSrc,
		"Program.cs": programSrc,
		"Native.cs":  nativeSrc,
	} {
		stripped := stripUncheckedBlocks(src)
		if m := hashConsts.FindString(stripped); m != "" {
			t.Fatalf("%s has unwrapped hash/PRNG constant %s outside an unchecked block", name, m)
		}
	}

	// 5. ModeLayout must run VerifyFnv1a64 with the standard FNV-1a 64 vectors and SplitMix64 check.
	verifyBlock := extractBraceBlock(t, programSrc, "static void VerifyFnv1a64()")
	for _, vec := range []string{"0xcbf29ce484222325UL", "0xaf63dc4c8601ec8cUL", "0x85944171f73967e8UL", "Mix(", "rng.Next()"} {
		if !strings.Contains(verifyBlock, vec) {
			t.Fatalf("VerifyFnv1a64 in Program.cs must check %q:\n%s", vec, verifyBlock)
		}
	}
	layoutBlock := extractBraceBlock(t, programSrc, "static void ModeLayout()")
	if !strings.Contains(layoutBlock, "VerifyFnv1a64()") {
		t.Fatalf("ModeLayout in Program.cs must invoke VerifyFnv1a64()")
	}
}

func extractBraceBlock(t *testing.T, src, anchor string) string {
	t.Helper()
	idx := strings.Index(src, anchor)
	if idx < 0 {
		t.Fatalf("anchor %q not found", anchor)
	}
	openRel := strings.IndexByte(src[idx:], byte(123))
	if openRel < 0 {
		t.Fatalf("opening brace after %q not found", anchor)
	}
	start := idx + openRel
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case byte(123):
			depth++
		case byte(125):
			depth--
			if depth == 0 {
				return src[idx : i+1]
			}
		}
	}
	t.Fatalf("unclosed brace block for %q", anchor)
	return ""
}

func stripUncheckedBlocks(src string) string {
	re := regexp.MustCompile(`\bunchecked\s*\{`)
	out := src
	for {
		loc := re.FindStringIndex(out)
		if loc == nil {
			break
		}
		braceStart := loc[1] - 1
		depth := 0
		end := -1
		for i := braceStart; i < len(out); i++ {
			switch out[i] {
			case byte(123):
				depth++
			case byte(125):
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break
		}
		out = out[:loc[0]] + out[end:]
	}
	return out
}
