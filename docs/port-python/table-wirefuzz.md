# Python table wire-fuzz notes

The form-1 array reader decodes an older scalar element kind at the wire
width before writing the current kind. One widening is counted per field.

For the widening read, a bulk `struct.unpack` followed by the range check was
compared with a per-element `int.from_bytes` walk over identical signed
16-bit payloads, on Linux with the pinned interpreters. The minimum of five
runs, in microseconds per read, was:

| elements | Python 3.15.2 walk | Python 3.15.2 bulk | Python 3.11.18 walk | Python 3.11.18 bulk |
| ---: | ---: | ---: | ---: | ---: |
| 16 | 2.620 | 0.691 | 3.034 | 0.771 |
| 1024 | 155.958 | 25.505 | 168.501 | 27.734 |

Both shapes included the range check and result list. The bulk form was kept.

The verdict pipe dispatches ordinary variable and declared fixed file roots,
message batches, and retaining reads and writes through their generated public
APIs. The retaining negative control replaces retaining write-back with ordinary
save on a pinned retained-record vector; the shared Go oracle must report a
value mismatch, on both pinned interpreters. This focused control does not by
itself establish the full roster or seeded release runs.

The pull-request support plan partitions the full-roster gate into twelve
independent rows: current and floor interpreters, normal and development
modes, and plain, mixed-builder and retaining arms. Each row runs every
applicable enumerated mutation and pinned vector plus `N` random mutations at
`SEED`; neither roster nor count is reduced by the partition. Defaults are
`N=100000` and `SEED=24845619678`. The partition control removes an arm from
a copied make fragment and requires the coverage check to identify the gap.
The nightly target uses the same rows at `N=500000` for the default seed and
seed 2. Release-tier plain and retain runs additionally require seeds 1, 2 and
3 at `N=100000`. Row timings and the release-tier registration are not yet
established by this draft.
