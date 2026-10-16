package javatable

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableSaveAndMeasureOwnNoScratchJava pins, in the emitter's own
// output, the allocation shape the java allocation gate measures (make
// tables-java-allocator, rows 3 and 4 save): the variable form's save writes
// every nested body IN PLACE into the Saver's one buffer and frames it after
// (no `new byte[1 << 20]` scratch per call, per node or per body, no sub
// writer), and loadMeasure walks every nested body in place on the Measure's
// one reader (no Reader per body, no NodeTable, no node body copied). The
// gate counts the bytes; this test names the line that would bring a
// per-node allocation back, without a JDK. It reads generated text only: no
// time, sockets, network or outside service.
func TestVariableSaveAndMeasureOwnNoScratchJava(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"pointers", "blobs"} {
		u := unitFromDir(t, filepath.Join("..", "..", "..", "tables", dir))
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		codecs := 0
		for name, data := range files {
			src := string(data)
			switch {
			case name == "TableVariable.java":
				for _, sig := range []string{
					"public static int trailerBase(byte[] data, Report report) {",
					"public static int bodyExtent(byte[] buf, int idCount, int off, int limit) {",
					"public static int skipExtent(byte[] buf, int kind, int off, int limit) {",
					"public static int lebEnd(byte[] buf, int off, int limit) {",
					"public static void measureNodes(Reader r, Measure m, long align) {",
					"public void frame(int start) {",
					"public void frameField(int start, long ref, int kind) {",
					"public void frameBody(int start, int drop) {",
					"public void frameEntry(int start, long key, int drop) {",
					"public void frameKeyed(int start, int kind, int count) {",
					"public void putUtf8(String s) {",
				} {
					body := javaMethodBody(t, name, src, sig)
					if strings.Contains(body, "new ") {
						t.Errorf("%s/%s: %q allocates on the measured path:\n%s", dir, name, sig, body)
					}
				}
				if strings.Contains(src, "nodesRaw") || strings.Contains(src, "lebAt(") {
					t.Errorf("%s/%s: a retired copying scan (nodesRaw, lebAt) is back", dir, name)
				}
			case strings.HasSuffix(name, "Variable.java"):
				codecs++
				for _, sig := range []string{
					"public static byte[] save(Value v, TableVariable.Saver saver) {",
					"public static void writeFields(TableVariable.Writer w, TableVariable.NodeGraph graph, Value v) {",
					"public static boolean buildGraph(TableVariable.NodeGraph g, Value v) {",
					"public static long loadMeasure(byte[] data, TableVariable.Measure m) {",
					"static long measureFields(TableVariable.Reader r, TableVariable.Measure nt, TableVariable.Report report) {",
				} {
					body := javaMethodBody(t, name, src, sig)
					for _, bad := range []string{"new ", ".sub(", ".body()", "getBytes(", " : v."} {
						if bad == "new " && strings.Contains(sig, "save(") {
							// the answer is the one array save hands back
							body = strings.Replace(body, "java.util.Arrays.copyOf(w.buf, w.off)", "", 1)
						}
						if strings.Contains(body, bad) {
							t.Errorf("%s/%s: %q carries %q, an allocation per call or per node:\n%s", dir, name, sig, bad, body)
						}
					}
				}
			}
		}
		if codecs == 0 {
			t.Fatalf("%s: no <Table>Variable.java emitted", dir)
		}
	}
}

// TestVariableKeyedAndUnionSaveNoScratch pins the paths covered by the
// keyed_config.bin and arms_gate.bin allocation rows. The broader examples
// package has other, unrelated allocation forms, so check these methods by
// their emitted boundaries rather than applying the graph-only assertions to
// every example codec.
func TestVariableKeyedAndUnionSaveNoScratch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dir, file string
		methods   []string
	}{
		{"examples", "KeyedConfigVariable.java", []string{"static void writeKeyedField_", "static int writeKeyedPayload_", "static boolean isDefault(Value v,TableVariable.Writer w,TableVariable.NodeGraph graph)"}},
		{"arms", "ReachVariable.java", []string{"static void writePayload(TableVariable.Writer w,TableVariable.NodeGraph graph,Value v)"}},
	} {
		u := unitFromDir(t, filepath.Join("..", "..", "..", "tables", tc.dir))
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		src := string(files[tc.file])
		if src == "" {
			t.Fatalf("%s: no generated %s", tc.dir, tc.file)
		}
		for _, method := range tc.methods {
			at := strings.Index(src, method)
			if at < 0 {
				t.Errorf("%s: missing %s", tc.file, method)
				continue
			}
			end := strings.Index(src[at:], "\n    static ")
			if end < 0 {
				end = len(src) - at
			}
			if strings.Contains(src[at:at+end], "new byte[1 << 20]") {
				t.Errorf("%s: %s allocates a scratch array", tc.file, method)
			}
		}
	}
}

// javaMethodBody answers the text of one generated method, from its
// signature to the closing brace at the method's own indent.
func javaMethodBody(t *testing.T, file, src, sig string) string {
	t.Helper()
	i := strings.Index(src, sig)
	if i < 0 {
		t.Fatalf("%s: no %q", file, sig)
	}
	line := strings.LastIndex(src[:i], "\n") + 1
	indent := src[line:i]
	end := strings.Index(src[i:], "\n"+indent+"}\n")
	if end < 0 {
		t.Fatalf("%s: %q has no closing brace", file, sig)
	}
	return src[i : i+end]
}
