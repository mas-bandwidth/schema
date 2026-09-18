RESULT df7-go-measure-2 sha=bfdd2c9e7f8a
BRANCH: rowan/df7-go-measure-2
status: ok
measured: bench/results/df7-go-measure-2.csv (two go rows: bench_mixed write + round_trip)
go bench_mixed write      median 540,585 msg/s (min 525,292, max 613,997) 225.81 MiB/s spread 16.41%
go bench_mixed round_trip median 835,676 msg/s (min 582,310, max 846,637) 349.07 MiB/s spread 31.63%
corpus_id: fd0748942dc6ae09
toolchain: go1.23.4 (cached toolchain via GOTOOLCHAIN, /usr/bin/go launcher)
runtimes: serialize.go a1f6ab7 (main), serialize 6c692bf (main)
change: add runtime.KeepAlive escape barrier on gBitsBuffer/gBitsVariants in bench/go/bits.go, matching main.go:310/341 and bench_main.cpp:511
see: notes.txt
