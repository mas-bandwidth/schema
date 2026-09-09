package cstable

import (
	"strings"
	"testing"
)

func TestCsharpRootArrayElemCacheShape(t *testing.T) {
	requiredPatterns := []string{
		"int cachedElemSlots = !measure && !type.Variable ? 256 : 0;",
		"Span<long> rootElemSizes = stackalloc long[cachedElemSlots];",
		"long n = 1 + BodySize(value, type, ref ids, rootPayloadSizes, rootElemSizes) + 8L * ids.Count + 8;",
		"w.Byte(1); WriteBody(ref w, value, type, ref ids, true, rootPayloadSizes, rootElemSizes);",
		"static long BodySize(object value, TableTypeInfo type, ref Ids ids, scoped Span<long> rootPayloadSizes = default, scoped Span<long> rootElemSizes = default)",
		"if (f.IsArray && f.KeyId == null && f.Kind == 13 && !rootElemSizes.IsEmpty)",
		"int take = Math.Min(c, Math.Max(0, rootElemSizes.Length - elemOffset));",
		"elemCache = rootElemSizes.Slice(elemOffset, take);",
		"elemOffset += take;",
		"long payload = PayloadSize(value, f, ref ids, elemCache);",
		"static void WriteBody(ref Writer w, object value, TableTypeInfo type, ref Ids ids, bool root = false, scoped ReadOnlySpan<long> rootPayloadSizes = default, scoped ReadOnlySpan<long> rootElemSizes = default)",
		"WritePayload(ref w, value, f, ref ids, elemCache);",
		"long childBody = (!elemCache.IsEmpty && i < elemCache.Length) ? elemCache[i] : BodySize(child, f.Table, ref ids);",
		"w.Var((ulong)childBody);",
		"WriteBody(ref w, child, f.Table, ref ids);",
	}
	for _, pattern := range requiredPatterns {
		if !strings.Contains(tableWireSource, pattern) {
			t.Fatalf("tableWireSource missing expected root array element cache pattern: %q", pattern)
		}
	}
}
