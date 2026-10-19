package elixir

import (
	"strings"
	"testing"
)

func TestFastReadNoMatchSuppressionRequiresImmediateAlign(t *testing.T) {
	t.Parallel()
	aligned, err := emitUnit(t, map[string]string{
		"Bench.schema": "package bench\n\ntype BenchPacket {\n    align\n    blob [17]uint8\n}\n",
	})
	if err != nil {
		t.Fatalf("Generate aligned BenchPacket: %v", err)
	}
	want := "# This field follows align directly, so every call has bits_read divisible by eight.\n  @dialyzer {:no_match, [r_bench_packet_blob_align: 5]}"
	if !strings.Contains(string(aligned["Bench.ex"]), want) {
		t.Fatalf("aligned array helper lacks its function-scoped Dialyzer justification:\n%s", aligned["Bench.ex"])
	}

	unaligned, err := emitUnit(t, map[string]string{
		"Bench.schema": "package bench\n\ntype BenchPacket {\n    prefix bits(3)\n    blob [17]uint8\n}\n",
	})
	if err != nil {
		t.Fatalf("Generate unaligned BenchPacket: %v", err)
	}
	if strings.Contains(string(unaligned["Bench.ex"]), "@dialyzer {:no_match, [r_bench_packet_blob_align: 5]}") {
		t.Fatal("unaligned array helper must retain Dialyzer's no-match warning")
	}
}
