package luautable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var luauFixedBOverlayRows = []string{
	"port/luau/fixed/signed-fixed-point",
	"port/luau/fixed/unsigned-fixed-point",
	"port/luau/fixed/flags-values",
	"port/luau/fixed/enum-values",
	"port/luau/fixed/utf8-values",
	"port/luau/fixed/byte-buffers",
	"port/luau/fixed/nested-types",
	"port/luau/fixed/nested-fixed-tables",
	"port/luau/fixed/fixed-arrays",
}

var (
	luauOverlayIDRx    = regexp.MustCompile(`^\(:id\s+"([^"]+)"`)
	luauOverlayStateRx = regexp.MustCompile(`:state\s+:([A-Za-z0-9_-]+)`)
	luauOverlayRevRx   = regexp.MustCompile(`:tested-revision\s+"([^"]*)"`)
	luauOverlayEvRx    = regexp.MustCompile(`"([^"]*)"`)
	luauTestFuncRx     = regexp.MustCompile(`(?m)^func\s+([A-Za-z0-9_]+)\s*\(`)
)

// TestLuauOverlayFixedBRowsAreEvidenced guards the nine group B overlay rows:
// each must be present in docs/roadmap-evidence/luau.sexp, be :done, carry a
// full forty-hex :tested-revision, and cite test functions that exist in the
// package's test sources. It reads files only.
func TestLuauOverlayFixedBRowsAreEvidenced(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	overlayBytes, err := os.ReadFile(filepath.Join(root, "docs", "roadmap-evidence", "luau.sexp"))
	if err != nil {
		t.Fatal(err)
	}

	rows := map[string]string{}
	for line := range strings.SplitSeq(string(overlayBytes), "\n") {
		m := luauOverlayIDRx.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		rows[m[1]] = line
	}

	for _, id := range luauFixedBOverlayRows {
		line, ok := rows[id]
		if !ok {
			t.Errorf("%s: missing from docs/roadmap-evidence/luau.sexp", id)
			continue
		}
		state := luauOverlayStateRx.FindStringSubmatch(line)
		if state == nil || state[1] != "done" {
			t.Errorf("%s: :state is not :done (%q)", id, state)
			continue
		}
		rev := luauOverlayRevRx.FindStringSubmatch(line)
		if rev == nil || !isFortyHex(rev[1]) {
			t.Errorf("%s: :tested-revision is not a full forty-hex sha (%q)", id, rev)
		}
		evidence := luauOverlayEvidence(line)
		if len(evidence) == 0 {
			t.Errorf("%s: has no :evidence strings", id)
			continue
		}
		for _, ev := range evidence {
			path, test, ok := strings.Cut(ev, ": ")
			if !ok {
				t.Errorf("%s: evidence %q is not \"<file>: <test>\"", id, ev)
				continue
			}
			name, _, _ := strings.Cut(test, "/")
			name = strings.TrimSpace(strings.SplitN(name, ",", 2)[0])
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Errorf("%s: evidence file %s: %v", id, path, err)
				continue
			}
			if !mentionsTestFunc(src, name) {
				t.Errorf("%s: evidence names %s, which is not a test function in %s", id, name, path)
			}
		}
	}
}

func isFortyHex(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func luauOverlayEvidence(line string) []string {
	at := strings.Index(line, ":evidence")
	if at < 0 {
		return nil
	}
	rest := line[at:]
	if end := strings.Index(rest, ":tested-revision"); end >= 0 {
		rest = rest[:end]
	}
	var out []string
	for _, m := range luauOverlayEvRx.FindAllStringSubmatch(rest, -1) {
		out = append(out, m[1])
	}
	return out
}

func mentionsTestFunc(src []byte, name string) bool {
	for _, m := range luauTestFuncRx.FindAllStringSubmatch(string(src), -1) {
		if m[1] == name {
			return true
		}
	}
	return false
}
