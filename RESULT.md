RESULT df7-go-measure-1 sha=17d211f988de
BRANCH: rowan/df7-go-measure-1
status: done
head: 740b6018 (rowan/df7-go-measure-1)
command: bench/run.sh --only go --quick --out bench/results/df7-go-measure-1.csv
csv: bench/results/df7-go-measure-1.csv (27 lines; two go data rows)
go bench_mixed write:      median 967,958 msg/s (404.33 MB/s), spread 56.58%
go bench_mixed round_trip: median 291,260 msg/s (121.66 MB/s), spread 28.21%
corpus_id: fd0748942dc6ae09
notes: notes.txt
caveat: box-pinned go1.26.5 was blocked by the wall (Permission denied); measured with Go 1.23.4 and sandbox-local serialize/serialize.go checkouts, both §3.5 build-verified.
