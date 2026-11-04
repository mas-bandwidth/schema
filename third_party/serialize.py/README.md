# serialize.py

A bitpacking serializer for Python, wire compatible with the C++
[serialize](https://github.com/mas-bandwidth/serialize) library and every other
member of its family: the same values produce the same bytes.

The wire is serialize's [STANDARD.md](STANDARD.md), **format version 2.0**.
STANDARD.md, IMPLEMENTING.md, the conformance corpus in
[conformance/](conformance/) and its negative controls in
[conformance-controls/](conformance-controls/) are vendored verbatim from
mas-bandwidth/serialize `v2.1.1` (commit `bd3d67e23c8a`), each file held to its
sha256 in [VENDORED](VENDORED). The library passes every vector with its stated
refusal reason, and every sabotage control fails exactly its `.expected` list.

**This repository is staged** at `third_party/serialize.py/` in
[mas-bandwidth/schema](https://github.com/mas-bandwidth/schema), which uses it
as its Python leg's bit layer, until `mas-bandwidth/serialize.py` exists; the
directory moves there as it stands.

Pure Python, standard library only, CPython 3.11 and newer.

## Use

One serialize function per message, written once against `Stream`, reads,
writes and measures. A scalar travels in a `Ref`:

```python
from serialize import ReadStream, Ref, Stream, WriteStream

def serialize_player(s: Stream, health: Ref[int], name: Ref[str]) -> bool:
    return s.serialize_int(health, 0, 100) and s.serialize_string(name, 32)

w = WriteStream(64)                        # capacity in bytes, a multiple of 8
serialize_player(w, Ref(87), Ref("golden"))
w.serialize_end()
w.flush()
packet = w.data()

r = ReadStream(packet)
health, name = Ref(0), Ref("")
if not (serialize_player(r, health, name) and r.serialize_end()):
    print(r.refusal())                     # "past_end", "invalid_utf8", ...
```

**A read refusal is a value.** Every read returns `False` and leaves its `Ref`
unwritten; the stream keeps the first refusal's reason, from the closed list in
`Refusal`, and every later read refuses too. A reader never raises on any input,
and a bad declaration on read (a buffer size below 1, `min > max`) refuses as
`value_out_of_range`. `reject()` is the caller's own refusal (`rejected`), and
a nested `serialize_object` whose function returns `False` latches it too.

**Write and measure misuse raises `WriteMisuse`**, a `ValueError`, at the call
and before any bit is written: a value outside its range or bit width, a
non-finite float, text that is not encodable as UTF-8, holds a NUL or does not
fit its buffer, a bad declaration, a write that does not fit the room left, and
`reject()` on a writer. Python has no release build, so these checks run on
every call. A writer never latches a refusal.

Values: integers of every width are `int`, `float` and `double` are `float`
(a `float` field carries the float32 nearest the value), `bytes` reads as
`bytes` and writes from any bytes-like object, and `string` is `str`, UTF-8 on
the wire. `MeasureStream` returns a bound that holds at every starting bit
position. `copy_string` bounds text for a `serialize_string`.

## Test

```
make test PYTHON_FLOOR=/path/to/python3.11 PYTHON=/path/to/python3.15 MYPY=/path/to/mypy
```

Runs every test under both interpreters, mypy in strict mode, and the corpus
runner's negative control.

BSD 3-Clause license.
