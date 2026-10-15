package gotable

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestGoTableWriterSpeedTable is the SPEED GATE of the card
// go-table-writers-refuse-what-their-readers-refuse. A writer that refuses
// what its reader refuses must not cost the write path more than the run's own
// spread (docs/USAGE.md "Writes are the caller's responsibility").
//
// It compiles the pong shape (a VARIABLE table: a string and three ranged
// uint32) and the done-record shape (a FIXED table: a counted array of
// completions, each three strings and an int64) into a probe, runs their save
// benchmarks with -bench -count=10, and logs one line per benchmark: the
// minimum, the median and the maximum ns/op of the ten. A change lands only if
// each after-median sits inside the before-run's minimum..maximum, measured on
// the same machine.
//
// It is a FUNCTIONAL test, not a unit test: it reads the real clock. It is
// skipped unless NOVA_WRITER_BENCH is set, so the unit tier stays clock-free.
func TestGoTableWriterSpeedTable(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_WRITER_BENCH") == "" {
		t.Skip("functional benchmark: set NOVA_WRITER_BENCH=1 to run it")
	}
	run := func(schema, probe string) string {
		out, err := runGeneratedResult(t, schema, probe, "-run=^$", "-bench=.", "-benchtime=200ms", "-count=10")
		if err != nil {
			t.Fatalf("benchmarks: %v\n%s", err, out)
		}
		return string(out)
	}
	// Two units: a unit with a variable `table` is regional and carries no
	// fixed form, so the two shapes cannot share one.
	out := run(`package probe
table Pong {
    nonce   string(32)
    queue   uint32 | min = 0, max = 100000
    working uint32 | min = 0, max = 10000
    width   uint32 | min = 0, max = 1024
}
`, `package probe
import ("testing")

func BenchmarkPongSave(b *testing.B) {
	var v Pong
	PongReset(&v)
	v.NonceLength = int32(copy(v.Nonce[:], "0123456789abcdef0123456789abcdef"))
	v.Queue, v.Working, v.Width = 4200, 77, 512
	buf := make([]byte, 4096)
	if PongSave(&v, buf) <= 0 {
		b.Fatal("the lawful pong did not save")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if PongSave(&v, buf) <= 0 {
			b.Fatal("save")
		}
	}
}
`) + run(`package probe
fixed table Completion {
    kind string(16)
    card string(515)
    row  string(135)
    at   int64
}
fixed table DoneRecord {
    epoch  uint64
    cursor string(41)
    done   [..95]Completion
}
`, `package probe
import ("testing")

func doneRecord(n int) DoneRecord {
	var v DoneRecord
	DoneRecordReset(&v)
	v.Epoch = 21
	v.CursorLength = int32(copy(v.Cursor[:], "1760000000000-0"))
	for i := 0; i < n; i++ {
		c := &v.Done[i]
		c.KindLength = int32(copy(c.Kind[:], "work"))
		c.CardLength = int32(copy(c.Card[:], "go-table-writers-refuse-what-their-readers-refuse.w4"))
		c.RowLength = int32(copy(c.Row[:], "friend.rowan"))
		c.At = int64(1760000000000000000 + i)
	}
	v.DoneCount = int32(n)
	return v
}

func benchDone(b *testing.B, n int) {
	values := []DoneRecord{doneRecord(n)}
	buf := make([]byte, DoneRecordFixedMeasure(1))
	if DoneRecordFixedSave(values, buf) != int64(len(buf)) {
		b.Fatal("the lawful done record did not save")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if DoneRecordFixedSave(values, buf) < 0 {
			b.Fatal("save")
		}
	}
}

func BenchmarkDoneRecordFixedSave20(b *testing.B) { benchDone(b, 20) }
func BenchmarkDoneRecordFixedSave95(b *testing.B) { benchDone(b, 95) }
`)
	line := regexp.MustCompile(`^(Benchmark\S+?)(?:-\d+)?\s+\d+\s+([0-9.]+) ns/op`)
	samples := map[string][]float64{}
	var order []string
	for _, l := range strings.Split(out, "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		ns, _ := strconv.ParseFloat(m[2], 64)
		if _, seen := samples[m[1]]; !seen {
			order = append(order, m[1])
		}
		samples[m[1]] = append(samples[m[1]], ns)
	}
	if len(order) != 3 {
		t.Fatalf("expected three benchmarks, parsed %v\n%s", order, out)
	}
	t.Logf("benchmark            n    min ns/op  median ns/op    max ns/op")
	for _, name := range order {
		s := samples[name]
		sort.Float64s(s)
		t.Logf("SPEED %-28s %2d %12.1f %12.1f %12.1f", name, len(s), s[0], (s[len(s)/2-1]+s[len(s)/2])/2, s[len(s)-1])
	}
}
