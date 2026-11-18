# serialize.rb

A bitpacking serializer for Ruby, wire compatible with the C++
[serialize](https://github.com/mas-bandwidth/serialize) library and every other
member of its family: the same values produce the same bytes.

The wire is serialize's [STANDARD.md](STANDARD.md), **format version 2.0**.
STANDARD.md, IMPLEMENTING.md, the conformance corpus in
[conformance/](conformance/) and its negative controls in
[conformance-controls/](conformance-controls/) are vendored verbatim from
mas-bandwidth/serialize `v2.1.1` (commit `bd3d67e23c8a`), each file held to its
sha256 in [VENDORED](VENDORED). The library passes every vector with its stated
refusal reason, and every sabotage control fails exactly its `.expected` list.

Pure Ruby, standard library only, CRuby 3.4 and newer. Every file is silent
under `ruby -w`.

## Use

One serialize function per message, written once against `Serialize::Stream`,
reads, writes and measures. A scalar travels in a `Serialize::Ref`:

```ruby
require 'serialize'

def serialize_player(s, health, name)
  s.serialize_int(health, 0, 100) && s.serialize_string(name, 32)
end

w = Serialize::WriteStream.new(64)         # capacity in bytes, a multiple of 8
serialize_player(w, Serialize::Ref.new(87), Serialize::Ref.new('golden'))
w.serialize_end
w.flush
packet = w.data                            # a binary String

r = Serialize::ReadStream.new(packet)
health = Serialize::Ref.new
name = Serialize::Ref.new
p r.refusal unless serialize_player(r, health, name) && r.serialize_end
```

A nested object is a block: `s.serialize_object { |st| ... }` succeeds when the
block returns `true`.

**A read refusal is a value.** Every read returns `false` and leaves its `Ref`
unwritten; the stream keeps the first refusal's reason, a Symbol from the
closed list in `Serialize::Refusal` (`:past_end`, `:invalid_utf8`, ...), and
every later read refuses too. A reader never raises on any input, and a bad
declaration on read (a buffer size below 1, `min > max`) refuses as
`:value_out_of_range`. `reject` is the caller's own refusal (`:rejected`), and
a nested `serialize_object` whose block does not return `true` latches it too.
`Serialize.refusal_name` gives the family's spelling of a reason.

**Write and measure misuse raises `Serialize::WriteMisuse`**, an
`ArgumentError`, at the call and before any bit is written: a value outside its
range or bit width, a non-finite float, text that is not valid UTF-8, holds a
NUL or does not fit its buffer, a bad declaration, a write that does not fit the
room left, and `reject` on a writer. Ruby has no release build, so these checks
run on every call. A writer never latches a refusal.

Values: integers of every width are `Integer`; `float` and `double` are `Float`
(a `float` field carries the float32 nearest the value, rounded as IEEE 754
rounds); `bytes` reads as a binary (ASCII-8BIT) String and writes the bytes of
any String; `string` is a UTF-8 String, and on write a UTF-8 or US-ASCII String
whose bytes are valid UTF-8. `MeasureStream` returns a bound that holds at every
starting bit position. `Serialize.copy_string` bounds text for a
`serialize_string`.

## Test

```
make dist-ruby && make test
make test RUBY_FLOOR=/path/to/ruby-3.4/bin/ruby RUBY=/path/to/ruby-4.0/bin/ruby
```

Runs every test under both interpreters with warnings on (`-w`), RuboCop over
`lib/` and `test/` under the floor, and the corpus runner's negative control.
`RUBY_FLOOR` is the oldest CRuby series in normal maintenance and `RUBY` the
newest stable; the pins are CRuby 3.4.12 and 4.0.9, with RuboCop 1.92.2 in the
floor. `make dist-ruby` ([make/ruby-toolchain.mk](make/ruby-toolchain.mk))
builds both from their ruby-lang.org source tarballs into `dist/`, refusing
any tarball whose sha256 is not the pin, and installs RuboCop offline from the
hash-locked [make/ruby-gems.lock](make/ruby-gems.lock); `make
ruby-toolchain-check` asks each pin its own version. When `dist/` holds the
pins, `RUBY_FLOOR` and `RUBY` default to them; the environment or the command
line overrides, and with no `dist/` both fall back to `ruby` on PATH.
`RUBOCOP` defaults to the floor's own RuboCop gem.

## CI and releases

`.github/workflows/ci.yml` runs `make test` on the pins on a self-hosted Linux
runner and on macOS, and `make ci-check`, `make ci-check-negative-control` and
`make release-negative-control` beside them. The pins come from
`.github/actions/ruby`: a runner's prebuilt pins when they are the pins, else
a store or cache keyed on every version, sha256 and the lock; the cold build
runs in `toolchain.yml` (nightly and by hand), never in a self-hosted
per-commit job. `make ci-check` (`tools/checkci/check-ci.sh`, files only)
refuses a workflow that runs an interpreter from PATH, names a pin other than
`make/ruby-toolchain.mk`'s, runs the tests before the pins are restored, uses
an action by a mutable ref, lacks the fork guard, a self-hosted job without a
timeout (two minutes per commit), or a pull-request workflow that does not
cancel a superseded run; `make ci-check-negative-control` plants each fault in
a copy and requires a red by that rule's name.

A release `vX.Y.Z` is the tag `.github/workflows/release.yml` makes, dispatched
by hand on main (`gh workflow run release.yml --ref main -f
version=vX.Y.Z`): it runs `make test` on the pins, then `make release`
(`tools/release/release.sh`), which refuses by name a version that is not
`vMAJOR.MINOR.PATCH`, is not greater than the newest `v` tag, already exists,
or names a commit not on main, and otherwise tags that one commit and pushes
that tag alone. `make release-negative-control` is its dry run, in a scratch
repository under `build/`.

`test/run.rb` is the runner: it refuses a run that selects no test, and a run in
which Ruby printed a warning. `test/test_conformance.rb` runs the vendored
corpus (`make conformance` runs it at the root `SERIALIZE_CORPUS_DIR` names),
the eighteen sabotage controls (each a copy of `lib/serialize/core.rb` with one
refusal rule deleted, which must fail exactly its `.expected` list), and the
runner's own reversed witnesses. `make negative-control` requires the corpus
test to go red, by name, over a root with no vectors.

## Speed

`make bench` writes and reads each hot operation 200,000 times, best of three,
with YJIT off and then on. CRuby 4.0.9 on an Apple silicon (arm64) Mac, in
nanoseconds per call:

| operation          | write, YJIT off | read, YJIT off | write, YJIT on | read, YJIT on |
|--------------------|----------------:|---------------:|---------------:|--------------:|
| bits 5             |             224 |            188 |             68 |           107 |
| bits 32            |             325 |            192 |            151 |           115 |
| int [-1000, 1000]  |             321 |            294 |            117 |            69 |
| int64 full range   |             861 |            820 |            602 |           574 |
| float              |             441 |            251 |            221 |           128 |
| double             |             473 |            467 |            270 |           313 |
| compressed_float   |            1756 |           1274 |            999 |           744 |
| bytes 16           |             281 |            225 |             70 |           213 |

The writer's accumulator is an Integer that appends each whole 64-bit word to a
binary String; the reader holds its bytes as little-endian 32-bit words, each
an immediate Integer, and every read is checked against the stream's length
before it touches them.

## Design and roadmap

[DESIGN.md](DESIGN.md) gives the public surface and why each choice was made:
the accumulator, the refusal model, float32 and finite values, string
encodings and YJIT, with the measurements. [ROADMAP.md](ROADMAP.md), generated
from [docs/roadmap.sexp](docs/roadmap.sexp) by `make roadmap`, is the planned
work. `make docs-check` runs every `ruby` block of this page and DESIGN.md under
`ruby -w` and resolves every relative link.

BSD 3-Clause license.
