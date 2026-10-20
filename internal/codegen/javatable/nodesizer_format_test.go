package javatable

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNodeSizerIsInFormatterShape holds the emitted NODE_SIZER block to
// google-java-format 1.37.0 --aosp's own shape (make/java.mk: the generated
// Java is format-canonical, so a block the formatter would reflow is an
// emitter bug): the assignment breaks after `=`, the anonymous class sits on
// a continuation line at +8, size()'s parameters are one per line, and every
// if body is a braced block. The emitters are exercised on tables/arms (a
// TYPE_ID and a STRING_ID case), tables/blobs (a BYTES_ID case) and a table
// with no pointer closure at all (only `return 0;`).
func TestNodeSizerIsInFormatterShape(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	cases := []struct {
		name string
		unit func(*testing.T) (map[string][]byte, error)
		file string
		want string
	}{
		{
			name: "arms-gate",
			unit: func(t *testing.T) (map[string][]byte, error) {
				return Generate(unitFromDir(t, filepath.Join(repo, "tables", "arms")))
			},
			file: "GateVariable.java",
			want: `    private static final TableVariable.NodeSizer NODE_SIZER =
            new TableVariable.NodeSizer() {
                public long size(
                        long typeId,
                        TableVariable.Reader r,
                        int off,
                        int len,
                        TableVariable.Measure m) {
                    if (typeId == OnlyVariable.TYPE_ID) {
                        return OnlyVariable.measureNode(r, off, len);
                    }
                    if (typeId == TableVariable.STRING_ID) {
                        return (8 + len + 1 + ALIGN - 1) & -ALIGN;
                    }
                    return 0;
                }
            };
`,
		},
		{
			name: "blobs-catalog",
			unit: func(t *testing.T) (map[string][]byte, error) {
				return Generate(unitFromDir(t, filepath.Join(repo, "tables", "blobs")))
			},
			file: "CatalogVariable.java",
			want: `    private static final TableVariable.NodeSizer NODE_SIZER =
            new TableVariable.NodeSizer() {
                public long size(
                        long typeId,
                        TableVariable.Reader r,
                        int off,
                        int len,
                        TableVariable.Measure m) {
                    if (typeId == AssetVariable.TYPE_ID) {
                        return AssetVariable.measureNode(r, off, len);
                    }
                    if (typeId == TableVariable.STRING_ID) {
                        return (8 + len + 1 + ALIGN - 1) & -ALIGN;
                    }
                    if (typeId == TableVariable.BYTES_ID) {
                        return (8 + len + ALIGN - 1) & -ALIGN;
                    }
                    return 0;
                }
            };
`,
		},
		{
			name: "flat-return-zero",
			unit: func(t *testing.T) (map[string][]byte, error) {
				return Generate(unitFrom(t, "package probe\ntable Flat { n int32 }\n"))
			},
			file: "FlatVariable.java",
			want: `    private static final TableVariable.NodeSizer NODE_SIZER =
            new TableVariable.NodeSizer() {
                public long size(
                        long typeId,
                        TableVariable.Reader r,
                        int off,
                        int len,
                        TableVariable.Measure m) {
                    return 0;
                }
            };
`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := tc.unit(t)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			src, ok := files[tc.file]
			if !ok {
				t.Fatalf("%s not emitted", tc.file)
			}
			got := nodeSizerBlock(t, string(src))
			if got != tc.want {
				t.Fatalf("NODE_SIZER in %s is not in google-java-format's shape:\ngot:\n%s\nwant:\n%s", tc.file, got, tc.want)
			}
		})
	}
}

// nodeSizerBlock extracts the NODE_SIZER declaration through its closing `};`.
func nodeSizerBlock(t *testing.T, src string) string {
	t.Helper()
	const head = "    private static final TableVariable.NodeSizer NODE_SIZER ="
	start := strings.Index(src, head)
	if start < 0 {
		t.Fatal("NODE_SIZER not emitted")
	}
	rest := src[start:]
	lines := strings.SplitAfter(rest, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "};" {
			return strings.Join(lines[:i+1], "")
		}
	}
	t.Fatal("NODE_SIZER closing brace not found")
	return ""
}
