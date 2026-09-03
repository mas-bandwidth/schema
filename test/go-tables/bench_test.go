// THE BENCH: the representative FIXED table's three operations, so the Go
// port's numbers stand beside the C++ reference's over the same bytes.
//
// It is a LIKE-FOR-LIKE pair with test/tables/bench_main.cpp: same golden, same
// three operations, same order, same warm buffer. What it measures is the
// generated codec and nothing around it — no file I/O, no allocation, no
// framing — because the question the ratio answers is whether a Go consumer of
// this format pays for the language or for the format.
package schematables

import (
	"testing"

	"tabledemo"
)

func BenchmarkRootConfigLoad(b *testing.B) {
	bytes := wire(b, representative)
	var value tabledemo.RootConfig
	var report tabledemo.TableReport
	b.SetBytes(int64(len(bytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report = tabledemo.TableReport{}
		if !tabledemo.RootConfigLoad(&value, bytes, &report) {
			b.Fatal("the golden does not load")
		}
	}
}

func BenchmarkRootConfigMeasure(b *testing.B) {
	bytes := wire(b, representative)
	var value tabledemo.RootConfig
	if !tabledemo.RootConfigLoad(&value, bytes, nil) {
		b.Fatal("the golden does not load")
	}
	b.SetBytes(int64(len(bytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if tabledemo.RootConfigMeasure(&value) < 0 {
			b.Fatal("measure refused the value")
		}
	}
}

func BenchmarkRootConfigSave(b *testing.B) {
	bytes := wire(b, representative)
	var value tabledemo.RootConfig
	if !tabledemo.RootConfigLoad(&value, bytes, nil) {
		b.Fatal("the golden does not load")
	}
	buffer := make([]byte, tabledemo.RootConfigMeasure(&value))
	b.SetBytes(int64(len(buffer)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if tabledemo.RootConfigSave(&value, buffer) != int64(len(buffer)) {
			b.Fatal("save did not write measure's answer")
		}
	}
}

// the ROUND TRIP, which is the shape a server actually runs.
func BenchmarkRootConfigRoundTrip(b *testing.B) {
	bytes := wire(b, representative)
	var value tabledemo.RootConfig
	var report tabledemo.TableReport
	buffer := make([]byte, len(bytes)*2)
	b.SetBytes(int64(len(bytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report = tabledemo.TableReport{}
		if !tabledemo.RootConfigLoad(&value, bytes, &report) {
			b.Fatal("the golden does not load")
		}
		size := tabledemo.RootConfigMeasure(&value)
		if tabledemo.RootConfigSave(&value, buffer[:size]) != size {
			b.Fatal("save did not write measure's answer")
		}
	}
}
