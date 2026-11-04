"""The cross-language corpus instrument (STANDARD.md, "The conformance corpus").

Enumerates conformance/ at run time and runs every record as STANDARD.md,
"What a runner checks", requires:

* an accepted vector decodes the stated value (a bit pattern for the float
  family) and consumes exactly `consumed` bits;
* `writer canonical` re-writes the decoded values to the vector's bytes;
* `writer_input bits = 0x...` hands the compressed_float writer that float32
  and requires the vector's bytes;
* a refused vector refuses with its stated reason (the stream's first
  refusal), leaves the failing read's destination unwritten, and leaves the
  stream terminal: every later step refuses too, consuming no bits and writing
  nothing, and so does one more read of every step after the record, and an
  end;
* `measure_at_least` is a floor for MeasureStream.

Vector names are unique across the corpus; a malformed record (an unknown key,
a refusal with no reason or an unknown one) is red, and an operation with no
runner is red, never skipped.

The buffer contract: a ReadStream reads its bytes and never looks past them,
so the stream is presented with no slack.

THE CONTROLS (STANDARD.md, "A gate nobody has seen go red"): the vendored
conformance-controls/unknown-operation.txt must be rejected; and for each
refusal rule, a reader with that one rule deleted (a copy of _core.py with one
needle replaced, loaded as a module in place of the real one) runs the real
corpus and must fail exactly the vectors conformance-controls/<rule>.expected
names. A needle that does not match exactly once is a failure of the control.

THE CORPUS ROOT: the directory holding conformance/ and conformance-controls/.
It is this repository's root unless SERIALIZE_CORPUS_DIR names another. A root
that holds no vectors is red, by name.
"""

from __future__ import annotations

import os
import types
import unittest
from dataclasses import dataclass, field
from typing import Any, Callable

import serialize._core as core

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CORE_SOURCE = os.path.join(ROOT, "src", "serialize", "_core.py")


def corpus_root_of(value: str | None) -> str:
    """The corpus root for a SERIALIZE_CORPUS_DIR value; unset or empty is ROOT."""
    if not value:
        return ROOT
    return value


CORPUS_ROOT = corpus_root_of(os.environ.get("SERIALIZE_CORPUS_DIR"))


class RunnerError(Exception):
    """A vector the runner refuses: a disagreement, or a malformed record."""


def check(cond: bool, message: str) -> None:
    if not cond:
        raise RunnerError(message)


# The library under test. A control swaps in a sabotaged module.
class Lib:
    mod: Any = core


# ---------------------------------------------------------------------------
# numbers and bytes (STANDARD.md, "The vector format")
# ---------------------------------------------------------------------------


def parse_int(token: str) -> int:
    t = token.strip()
    check(t != "", "malformed number: empty")
    neg = t.startswith("-")
    body = t[1:] if neg else t
    if body[:2] in ("0x", "0X"):
        digits = body[2:]
        check(
            digits != "" and len(digits) <= 32 and all(c in "0123456789abcdefABCDEF" for c in digits),
            f"malformed hexadecimal number: {token}",
        )
        v = int(digits, 16)
    else:
        check(body.isdigit() and body.isascii(), f"malformed decimal number: {token}")
        v = int(body, 10)
    v = -v if neg else v
    check(-(1 << 128) < v < (1 << 128), f"number wider than 128 bits: {token}")
    return v


def parse_float_param(token: str) -> float:
    try:
        return float(token)
    except ValueError:
        raise RunnerError(f"malformed float parameter: {token}") from None


def unhex(text: str) -> bytes:
    compact = "".join(text.split())
    check(len(compact) % 2 == 0 and all(c in "0123456789abcdefABCDEF" for c in compact), f"malformed hex bytes: {text}")
    return bytes.fromhex(compact)


def hex_of(data: bytes) -> str:
    return " ".join(f"{b:02X}" for b in data)


# ---------------------------------------------------------------------------
# records
# ---------------------------------------------------------------------------

KNOWN_KEYS = frozenset(
    ("operation", "name", "param", "bytes", "expect", "consumed", "writer", "writer_input", "measure_at_least")
)


@dataclass
class Record:
    operation: str
    file: str
    line: int
    name: str | None = None
    params: list[tuple[str, str]] = field(default_factory=list)
    data: bytes | None = None
    expect: str | None = None
    reason: str | None = None
    expect_text: str = ""
    consumed: int | None = None
    writer: bool = False
    writer_input: int | None = None
    measure: int | None = None

    def param(self, name: str) -> str | None:
        for n, v in self.params:
            if n == name:
                return v
        return None


def parse_vectors_text(content: str, path: str) -> list[Record]:
    records: list[Record] = []
    cur: Record | None = None
    for line_no, line in enumerate(content.split("\n"), 1):
        where = f"{path}:{line_no}"
        if line.strip() == "":
            cur = None
            continue
        if line.startswith("#"):
            continue
        key, _, rest = line.strip().partition(" ")
        rest = rest.strip()
        check(key in KNOWN_KEYS, f"{where}: unknown key {key}")
        if key == "operation":
            check(cur is None, f"{where}: a second operation in one record")
            cur = Record(operation=rest, file=path, line=line_no)
            records.append(cur)
            continue
        check(cur is not None, f"{where}: {key} outside a record")
        assert cur is not None
        if key == "name":
            check(cur.name is None and rest != "", f"{where}: name")
            cur.name = rest
        elif key == "param":
            pname, eq, pval = rest.partition("=")
            check(eq == "=" and pname.strip() != "", f"{where}: malformed param")
            cur.params.append((pname.strip(), pval.strip()))
        elif key == "bytes":
            cur.data = unhex(rest)
        elif key == "expect":
            check(cur.expect is None, f"{where}: a second expect")
            if rest == "refused" or rest.startswith("refused "):
                reason = rest[len("refused") :].strip()
                check(reason != "", f"{where}: a refusal states no reason")
                check(reason in Lib.mod.REASONS, f"{where}: unknown refusal reason {reason}")
                cur.expect = "refused"
                cur.reason = reason
            else:
                kind, eq, value = rest.partition("=")
                kind = kind.strip()
                check(eq == "=" and kind in ("value", "bits"), f"{where}: malformed expect {rest}")
                cur.expect = kind
                cur.expect_text = value.strip()
        elif key == "consumed":
            check(rest.isdigit(), f"{where}: malformed consumed")
            cur.consumed = int(rest)
        elif key == "writer":
            check(rest == "canonical", f"{where}: writer {rest}")
            cur.writer = True
        elif key == "writer_input":
            k, eq, v = rest.partition("=")
            v = v.strip()
            check(
                k.strip() == "bits" and eq == "=" and v.startswith("0x") and len(v) == 10,
                f"{where}: malformed writer_input",
            )
            cur.writer_input = int(v[2:], 16)
        elif key == "measure_at_least":
            check(rest.isdigit(), f"{where}: malformed measure_at_least")
            cur.measure = int(rest)
    for rec in records:
        where = f"{path}:{rec.line}"
        check(rec.name is not None, f"{where}: a record has no name")
        check(rec.expect is not None, f"{where}: {rec.name} has no expect")
        check(rec.data is not None, f"{where}: {rec.name} has no bytes")
        if rec.expect != "refused":
            check(rec.consumed is not None, f"{where}: {rec.name} has no consumed")
    return records


def parse_vectors(path: str) -> list[Record]:
    with open(path, encoding="utf-8") as f:
        return parse_vectors_text(f.read(), path)


# ---------------------------------------------------------------------------
# steps: one spelling per operation (conformance/sequence.txt)
# ---------------------------------------------------------------------------


@dataclass
class Step:
    op: str
    args: tuple[Any, ...] = ()
    unstated: bool = False
    index: int = 0
    children: list[Step] = field(default_factory=list)


ARITY = {
    "bits": 1,
    "bool": 0,
    "align": 0,
    "float": 0,
    "double": 0,
    "uint128": 0,
    "end": 0,
    "int": 2,
    "int64": 2,
    "int128": 2,
    "fixed": 4,
    "int_relative": 1,
    "compressed_float": 3,
    "bytes": 1,
    "string": 1,
    "object": 1,
    "reject": 0,
}


def parse_step(op: str, words: list[str]) -> Step:
    if op not in ARITY:
        raise RunnerError(f"no runner for operation {op}")
    check(len(words) == ARITY[op], f"step {op} takes {ARITY[op]} parameters, got {len(words)}")
    if op == "compressed_float":
        return Step(op, tuple(parse_float_param(w) for w in words))
    return Step(op, tuple(parse_int(w) for w in words))


# the wire operations a single-operation record may name; reject and object
# are steps only
KNOWN_OPS = frozenset(ARITY) - {"object", "reject"}

SINGLE_PARAMS = {
    "bits": ("bits",),
    "int": ("min", "max"),
    "int64": ("min", "max"),
    "int128": ("min", "max"),
    "fixed": ("integer_bits", "fraction_bits", "min", "max"),
    "int_relative": ("previous",),
    "compressed_float": ("min", "max", "res"),
    "bytes": ("count",),
    "string": ("buffer_size",),
}


def build_steps(rec: Record) -> tuple[list[Step], int | None]:
    """The flat step list, and the index of the step a single record's value is for."""
    name = rec.name or "?"
    if rec.operation == "sequence":
        steps: list[Step] = []
        for pname, pval in rec.params:
            check(pname == "step", f"{name}: a sequence takes only step params, got {pname}")
            words = pval.split()
            check(len(words) > 0, f"{name}: an empty step")
            steps.append(parse_step(words[0], words[1:]))
        check(len(steps) > 0, f"{name}: a sequence with no steps")
        return steps, None
    op = rec.operation
    if op not in KNOWN_OPS:
        raise RunnerError(f"no runner for operation {op}")
    values: list[str] = []
    used = 0
    for pname in SINGLE_PARAMS.get(op, ()):
        v = rec.param(pname)
        check(v is not None, f"{name}: param {pname} missing")
        assert v is not None
        values.append(v)
        used += 1
    for pname, _ in rec.params:
        check(pname != "step", f"{name}: a step param outside a sequence")
    steps = []
    pb = rec.param("preceding_bits")
    if pb is not None:
        used += 1
        steps.append(Step("bits", (parse_int(pb),), unstated=True))
    check(used == len(rec.params), f"{name}: unexpected params for {op}")
    steps.append(parse_step(op, values))
    return steps, len(steps) - 1


def tree_of(steps: list[Step]) -> list[Step]:
    """`object <n>` wraps the next n steps."""
    pos = 0

    def take(limit: int | None) -> list[Step]:
        nonlocal pos
        nodes: list[Step] = []
        while pos < len(steps) and (limit is None or len(nodes) < limit):
            st = steps[pos]
            st.index = pos
            pos += 1
            if st.op == "object":
                st.children = take(st.args[0])
                check(len(st.children) == st.args[0], f"object {st.args[0]} wraps fewer steps")
            nodes.append(st)
        return nodes

    return take(None)


# ---------------------------------------------------------------------------
# running steps
# ---------------------------------------------------------------------------


class _Sentinel:
    def __repr__(self) -> str:
        return "<unwritten>"


SENTINEL = _Sentinel()


def call(s: Any, st: Step, holder: Any) -> bool:
    op, a = st.op, st.args
    if op == "bits":
        if a[0] == 0:
            # zero bits read and write nothing on a healthy stream; a failed
            # one answers False (terminal)
            if s.is_reading:
                if s.refusal() is not None:
                    return False
                holder.value = 0
            return True
        return bool(s.serialize_bits(holder, a[0]))
    if op == "bool":
        return bool(s.serialize_bool(holder))
    if op == "uint128":
        return bool(s.serialize_uint128(holder))
    if op == "int":
        return bool(s.serialize_int(holder, a[0], a[1]))
    if op == "int64":
        return bool(s.serialize_int64(holder, a[0], a[1]))
    if op == "int128":
        return bool(s.serialize_int128(holder, a[0], a[1]))
    if op == "fixed":
        return bool(s.serialize_fixed(holder, a[0], a[1], a[2], a[3]))
    if op == "int_relative":
        return bool(s.serialize_int_relative(a[0], holder))
    if op == "float":
        return bool(s.serialize_float(holder))
    if op == "double":
        return bool(s.serialize_double(holder))
    if op == "compressed_float":
        return bool(s.serialize_compressed_float(holder, a[0], a[1], a[2]))
    if op == "bytes":
        return bool(s.serialize_bytes(holder, a[0]))
    if op == "string":
        return bool(s.serialize_string(holder, a[0]))
    if op == "align":
        return bool(s.serialize_align())
    if op == "end":
        return bool(s.serialize_end())
    raise RunnerError(f"no runner for operation {op}")


@dataclass
class Result:
    step: Step
    ok: bool = False
    consumed: int = 0
    holder: Any = None


def read_tree(s: Any, nodes: list[Step], out: dict[int, Result], in_object: bool = False) -> bool:
    """Reads every node, never stopping at a wire refusal, recording per step
    whether it passed, the bits it consumed, and its destination."""
    all_ok = True
    for st in nodes:
        before = s.bits_processed()
        r = Result(st, holder=Lib.mod.Ref(SENTINEL))
        out[st.index] = r
        if st.op == "object":
            children = st.children

            def body(stream: Any, children: list[Step] = children) -> bool:
                return read_tree(stream, children, out, True)

            r.ok = bool(s.serialize_object(body))
        elif st.op == "reject":
            if in_object:
                r.ok = False
                return False
            r.ok = bool(s.reject())
        else:
            r.ok = call(s, st, r.holder)
        r.consumed = s.bits_processed() - before
        all_ok = all_ok and r.ok
    return all_ok


def write_tree(s: Any, nodes: list[Step], values: dict[int, Any]) -> bool:
    for st in nodes:
        check(st.op != "reject", "a reject step appears only in refused vectors")
        if st.op == "object":
            children = st.children

            def body(stream: Any, children: list[Step] = children) -> bool:
                return write_tree(stream, children, values)

            if not s.serialize_object(body):
                return False
        elif not call(s, st, Lib.mod.Ref(values[st.index])):
            return False
    return True


def capacity_for(n: int) -> int:
    return max(8, (n + 7) // 8 * 8)


FLOAT_OPS = ("float", "double", "compressed_float")


def matches(st: Step, actual: Any, token: str, kind: str) -> bool:
    op = st.op
    if op in ("align", "end", "object", "reject"):
        return token in ("-", "0")
    if token == "-":
        return False
    if op in FLOAT_OPS:
        if not token.startswith("0x") or not isinstance(actual, float):
            return False
        want = int(token[2:], 16)
        got = Lib.mod.float64_bits(actual) if op == "double" else Lib.mod.float32_bits(actual)
        return bool(got == want)
    if kind == "bits":
        return False
    if op == "bool":
        return (token == "true" and actual is True) or (token == "false" and actual is False)
    if op == "bytes":
        return isinstance(actual, bytes) and actual == unhex(token)
    if op == "string":
        return isinstance(actual, str) and actual.encode("utf-8", "surrogatepass") == unhex(token)
    if not isinstance(actual, int) or isinstance(actual, bool):
        return False
    return actual == parse_int(token)


def render(st: Step, v: Any) -> str:
    if v is SENTINEL:
        return "<unwritten>"
    if isinstance(v, bytes):
        return f"[{hex_of(v)}]"
    if isinstance(v, str):
        return f"[{hex_of(v.encode('utf-8', 'surrogatepass'))}]"
    if isinstance(v, float):
        if st.op == "double":
            return f"0x{Lib.mod.float64_bits(v):016X}"
        return f"0x{Lib.mod.float32_bits(v):08X}"
    return repr(v)


def split_expect(text: str, n: int) -> list[str]:
    if n == 1:
        return [text]
    return [p.strip() for p in text.split("|")]


def run_writer_input(rec: Record, steps: list[Step]) -> None:
    check(
        len(steps) == 1 and steps[0].op == "compressed_float", f"{rec.name}: writer_input is for a single compressed_float"
    )
    assert rec.writer_input is not None and rec.data is not None
    a = steps[0].args
    ws = Lib.mod.WriteStream(capacity_for(len(rec.data)))
    value = Lib.mod.float32_from_bits(rec.writer_input)
    check(bool(ws.serialize_compressed_float(Lib.mod.Ref(value), a[0], a[1], a[2])), f"{rec.name}: writer_input write failed")
    ws.flush()
    got = ws.data()
    check(got == rec.data, f"{rec.name}: writer_input wrote [{hex_of(got)}], want [{hex_of(rec.data)}]")


def check_callback_refuses(rec: Record) -> None:
    """Over each refused object.txt vector's bytes: an object whose callback
    returns False with nothing refused inside latches rejected, and the stream
    is terminal after it."""
    assert rec.data is not None
    rs = Lib.mod.ReadStream(rec.data)
    check(rs.serialize_object(lambda s: False) is False, f"{rec.name}: an object whose callback returns False was accepted")
    check(rs.refusal() == "rejected", f"{rec.name}: an object callback's own refusal latched {rs.refusal()}, not rejected")
    probe = Lib.mod.Ref(SENTINEL)
    check(
        rs.serialize_bits(probe, 8) is False and probe.value is SENTINEL and rs.bits_processed() == 0,
        f"{rec.name}: a read after the callback's refusal was not refused cleanly",
    )
    called: list[bool] = []

    def body(s: Any) -> bool:
        called.append(True)
        return True

    check(
        rs.serialize_object(body) is False and not called,
        f"{rec.name}: an object after the callback's refusal was accepted or ran its callback",
    )
    check(rs.refusal() == "rejected", f"{rec.name}: the echoes changed rejected to {rs.refusal()}")


def run_record(rec: Record) -> None:
    steps, single = build_steps(rec)
    tree = tree_of(steps)
    name = rec.name or "?"
    assert rec.data is not None

    if rec.writer_input is not None:
        run_writer_input(rec, steps)

    rs = Lib.mod.ReadStream(rec.data)
    out: dict[int, Result] = {}
    ok = read_tree(rs, tree, out)

    if rec.expect == "refused":
        check(not ok, f"{name}: a refused vector was accepted")
        check(rs.refusal() == rec.reason, f"{name}: refused as {rs.refusal()}, the vector states {rec.reason}")
        first: int | None = None
        for i in range(len(steps)):
            r = out.get(i)
            if r is None:
                continue
            if first is None:
                if not r.ok and steps[i].op != "object":
                    first = i
                    check(
                        r.holder.value is SENTINEL,
                        f"{name}: step {i} refused but wrote its destination: {render(steps[i], r.holder.value)}",
                    )
            else:
                check(not r.ok, f"{name}: step {i} after the refusal was accepted")
                check(r.consumed == 0, f"{name}: step {i} after the refusal consumed bits")
                check(r.holder.value is SENTINEL, f"{name}: step {i} after the refusal wrote its destination")
        check(first is not None, f"{name}: no primitive step refused")
        before = rs.bits_processed()
        again: dict[int, Result] = {}
        check(not read_tree(rs, tree, again), f"{name}: the failed stream accepted a further read")
        for i in range(len(steps)):
            r = again.get(i)
            if r is not None:
                check(
                    not r.ok and r.consumed == 0 and r.holder.value is SENTINEL,
                    f"{name}: a further read of step {i} on the failed stream was not refused cleanly",
                )
        check(rs.serialize_end() is False, f"{name}: an end on the failed stream was accepted")
        check(rs.bits_processed() == before, f"{name}: the failed stream consumed bits")
        check(rs.refusal() == rec.reason, f"{name}: the echoes changed the first reason to {rs.refusal()}")
        if rec.file.endswith("object.txt"):
            check_callback_refuses(rec)
        return

    check(ok, f"{name}: an accepted vector was refused as {rs.refusal()}")
    check(
        rs.bits_processed() == rec.consumed, f"{name}: consumed {rs.bits_processed()}, the vector states {rec.consumed}"
    )
    values = {i: out[i].holder.value for i in range(len(steps))}
    kind = rec.expect or ""
    if single is not None:
        for i in range(len(steps)):
            if i != single:
                check(steps[i].unstated, f"{name}: a value with no expectation")
        check(
            matches(steps[single], values[single], rec.expect_text, kind),
            f"{name}: decoded {render(steps[single], values[single])}, the vector states {rec.expect_text}",
        )
    else:
        parts = split_expect(rec.expect_text, len(steps))
        check(len(parts) == len(steps), f"{name}: {len(parts)} expected values for {len(steps)} steps")
        for i, st in enumerate(steps):
            check(
                matches(st, values[i], parts[i], kind),
                f"{name}: step {i} ({st.op}) decoded {render(st, values[i])}, the vector states {parts[i]}",
            )

    if rec.writer:
        ws = Lib.mod.WriteStream(capacity_for(len(rec.data)))
        check(write_tree(ws, tree, values), f"{name}: the canonical write failed")
        check(ws.serialize_end() is True, f"{name}: the writer's end failed")
        ws.flush()
        got = ws.data()
        check(got == rec.data, f"{name}: the writer emitted [{hex_of(got)}], the vector states [{hex_of(rec.data)}]")

    if rec.measure is not None:
        ms = Lib.mod.MeasureStream()
        check(write_tree(ms, tree, values), f"{name}: the measure failed")
        check(
            ms.bits_processed() >= rec.measure,
            f"{name}: measured {ms.bits_processed()}, the vector requires at least {rec.measure}",
        )


# ---------------------------------------------------------------------------
# the corpus
# ---------------------------------------------------------------------------


@dataclass
class CorpusRun:
    counts: list[tuple[str, int, int, int]]  # file, records, accepted, refused
    total: int
    failed: list[tuple[str, str]]


def run_corpus(root: str | None = None) -> CorpusRun:
    directory = os.path.join(root or CORPUS_ROOT, "conformance")
    try:
        files = sorted(f for f in os.listdir(directory) if f.endswith(".txt"))
    except FileNotFoundError:
        files = []
    check(len(files) > 0, f"no conformance vectors found: {directory}/ is missing or empty")
    names: dict[str, str] = {}
    run = CorpusRun([], 0, [])
    for file in files:
        path = os.path.join(directory, file)
        records = parse_vectors(path)
        with open(path, encoding="utf-8") as f:
            declared = sum(1 for line in f if line.startswith("operation "))
        check(len(records) > 0 and len(records) == declared, f"{file}: parsed {len(records)} records, the file declares {declared}")
        accepted = refused = 0
        for rec in records:
            assert rec.name is not None
            check(rec.name not in names, f"{file}: vector name {rec.name} is not unique")
            names[rec.name] = file
            try:
                run_record(rec)
            except Exception as e:  # a sabotaged library may raise anything
                run.failed.append((rec.name, f"{type(e).__name__}: {e}"))
            if rec.expect == "refused":
                refused += 1
            else:
                accepted += 1
            run.total += 1
        run.counts.append((file, len(records), accepted, refused))
    return run


# ---------------------------------------------------------------------------
# the sabotage controls
# ---------------------------------------------------------------------------


@dataclass
class Patch:
    anchors: tuple[str, ...]
    needle: str
    replacement: str


@dataclass
class Control:
    name: str
    rule: str
    patches: list[Patch]


def _in(anchors: tuple[str, ...], needle: str, replacement: str) -> Patch:
    return Patch(anchors, needle, replacement)


READ = "class ReadStream("

CONTROLS = [
    Control(
        "align-padding",
        "readers must verify that the alignment padding bits are zero",
        [_in((READ, "def _check_align"), "if pad and self._take(pad) != 0:", "if pad and self._take(pad) != 0 and False:")],
    ),
    Control(
        "ranged-reconstruction",
        "a ranged int decodes to its offset plus min",
        [
            _in((READ, "def _read_int32"), "return min + offset", "return offset"),
            _in((READ, "def serialize_int_relative"), "current = previous + lo + offset", "current = previous + offset"),
        ],
    ),
    Control(
        "ranged-range",
        "a ranged int whose offset is past the span is refused",
        [
            _in((READ, "def _read_int32"), "if offset > span:", "if offset > span and False:"),
            _in((READ, "def serialize_int_relative"), "if offset > hi - lo:", "if offset > hi - lo and False:"),
        ],
    ),
    Control(
        "fixed-degenerate-terminal",
        "every read consults the failure state, zero bit reads included",
        [
            _in(
                (READ, "def serialize_fixed"),
                "if self._err is not None:\n            return False\n",
                "if False:\n            return False\n",
            )
        ],
    ),
    Control(
        "float-finite",
        "a float read refuses NaN and the infinities",
        [_in((READ, "def serialize_float"), "if (bits & 0x7F800000) == 0x7F800000:", "if (bits & 0x7F800000) == 0x7F800000 and False:")],
    ),
    Control(
        "double-finite",
        "a double read refuses NaN and the infinities",
        [
            _in(
                (READ, "def serialize_double"),
                "if (bits & 0x7FF0000000000000) == 0x7FF0000000000000:",
                "if (bits & 0x7FF0000000000000) == 0x7FF0000000000000 and False:",
            )
        ],
    ),
    Control(
        "compressed-float-finite",
        "a compressed float decode that is not finite is refused",
        [_in((READ, "def _read_compressed"), "if not math.isfinite(decoded):", "if not math.isfinite(decoded) and False:")],
    ),
    Control(
        "end-trailing-data",
        "an end refuses a whole byte after the object",
        [_in((READ, "def serialize_end"), "if remaining >= 8:", "if remaining >= 8 and False:")],
    ),
    Control(
        "end-padding",
        "an end refuses non-zero padding in the final byte",
        [
            _in(
                (READ, "def serialize_end"),
                "if remaining > 0 and self._take(remaining) != 0:",
                "if remaining > 0 and self._take(remaining) != 0 and False:",
            )
        ],
    ),
    Control(
        "int-relative-canonical",
        "the absolute tier refuses a difference a bounded tier carries",
        [_in((READ, "def serialize_int_relative"), "if v - previous <= LAST_BOUNDED_MAX:", "if v - previous <= LAST_BOUNDED_MAX and False:")],
    ),
    Control(
        "string-embedded-nul",
        "a string read refuses a zero byte among its payload",
        [_in((READ, "def serialize_string"), 'if b"\\x00" in payload:', 'if b"\\x00" in payload and False:')],
    ),
    Control(
        "string-utf8",
        "a string read refuses a payload that is not well formed UTF-8",
        [_in((READ, "def serialize_string"), 'text = payload.decode("utf-8")', 'text = payload.decode("utf-8", "surrogateescape")')],
    ),
    Control(
        "string-utf8-surrogate",
        "a string read refuses an encoded surrogate code point",
        [_in((READ, "def serialize_string"), 'text = payload.decode("utf-8")', 'text = payload.decode("utf-8", "surrogatepass")')],
    ),
    Control(
        # a reader whose ranged 64-bit integer passes through a double, as a
        # port backed by a double number type would
        "int64-through-double",
        "a 64 bit value is carried exactly, never through a double",
        [
            _in(
                (READ, "def serialize_int64"),
                "ref.value = min + offset",
                "ref.value = int(float(min + offset)) if float(min + offset) < 9223372036854775807.0 else INT64_MAX",
            )
        ],
    ),
    Control(
        # serialize_object consults the latch on entry and on exit, and either
        # alone refuses an empty object on a failed stream, so the control
        # deletes both
        "object-terminal",
        "a nested object on a failed stream refuses before its callback runs",
        [
            _in(
                (READ, "def serialize_object"),
                "if self._err is not None:\n            return False\n        if serialize(self) is True and self._err is None:",
                "if serialize(self) is True:",
            )
        ],
    ),
    Control(
        "object-latch-rejected",
        "a nested object that refuses on its own latches rejected",
        [_in((READ, "def serialize_object"), "return self._fail(REJECTED)", "return False")],
    ),
    Control(
        "reject-reason",
        "the reject entry point refuses as rejected",
        [_in((READ, "def reject"), "return self._fail(REJECTED)", "return self._fail(VALUE_OUT_OF_RANGE)")],
    ),
    Control(
        "wrong-reason",
        "a refusal names the rule that refused it",
        [
            _in(
                (READ, "def serialize_string"),
                'if b"\\x00" in payload:\n            return self._fail(EMBEDDED_NUL)',
                'if b"\\x00" in payload:\n            return self._fail(INVALID_UTF8)',
            )
        ],
    ),
]


def sabotaged_source(control: Control, source: str) -> str:
    for p in control.patches:
        at = 0
        for anchor in p.anchors:
            found = source.find(anchor, at)
            check(found >= 0, f"control {control.name}: the anchor {anchor!r} no longer matches")
            at = found + len(anchor)
        # the needle's region ends at the next definition at the anchor's level
        end = source.find("\n    def ", at)
        end = len(source) if end < 0 else end
        first = source.find(p.needle, at, end)
        check(first >= 0, f"control {control.name}: the needle no longer matches {p.needle!r}")
        check(
            source.find(p.needle, first + 1, end) < 0,
            f"control {control.name}: the needle matches more than once {p.needle!r}",
        )
        source = source[:first] + p.replacement + source[first + len(p.needle) :]
    return source


def load_sabotaged(control: Control) -> types.ModuleType:
    with open(CORE_SOURCE, encoding="utf-8") as f:
        source = sabotaged_source(control, f.read())
    mod = types.ModuleType(f"serialize._core_sabotaged_{control.name.replace('-', '_')}")
    exec(compile(source, f"{CORE_SOURCE} (sabotaged: {control.name})", "exec"), mod.__dict__)
    return mod


def with_library(mod: Any, fn: Callable[[], Any]) -> Any:
    saved = Lib.mod
    Lib.mod = mod
    try:
        return fn()
    finally:
        Lib.mod = saved


def read_expected(name: str) -> list[str]:
    path = os.path.join(CORPUS_ROOT, "conformance-controls", f"{name}.expected")
    with open(path, encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip() and not line.startswith("#")]


def run_control(control: Control) -> list[str]:
    """The problems with one control: empty when exactly its list failed."""
    expected = read_expected(control.name)
    check(len(expected) > 0, f"control {control.name}: the expected list names no vector")
    run = with_library(load_sabotaged(control), run_corpus)
    failed = dict(run.failed)
    problems = [f"{n} should have failed and passed" for n in expected if n not in failed]
    problems += [f"{n} failed and is not in the list: {failed[n]}" for n in sorted(failed) if n not in expected]
    return problems


# ---------------------------------------------------------------------------
# the runner's own reversed witnesses: each is a real vector with one thing
# wrong, which the runner must fail, beside its twin with that thing right
# ---------------------------------------------------------------------------

WITNESSES = [
    (
        "operation uint128\nname w\nbytes FF FF FF FF FF FF FF FF 00 00 00 00 00 00 00 00\nexpect value = 18446744073709551615\nconsumed 128\nwriter canonical\n",
        "operation uint128\nname w\nbytes FF FF FF FF FF FF FF FF 00 00 00 00 00 00 00 00\nexpect value = -1\nconsumed 128\n",
        "a 128-bit value compared on both halves",
    ),
    (
        "operation bits\nname w\nparam bits = 64\nbytes FF FF FF FF FF FF FF FF\nexpect value = 18446744073709551615\nconsumed 64\n",
        "operation bits\nname w\nparam bits = 64\nbytes FF FF FF FF FF FF FF FF\nexpect value = 18446744073709551614\nconsumed 64\n",
        "a 64-bit value compared exactly",
    ),
    (
        "operation int64\nname w\nparam min = -9223372036854775808\nparam max = 9223372036854775807\nbytes FF FF FF FF FF FF FF FF\nexpect value = 9223372036854775807\nconsumed 64\n",
        "operation int64\nname w\nparam min = -9223372036854775808\nparam max = 9223372036854775807\nbytes FF FF FF FF FF FF FF FF\nexpect value = 9223372036854775807\nconsumed 63\n",
        "the bits consumed",
    ),
    (
        "operation sequence\nname w\nparam step = bits 3\nbytes 05\nexpect value = 5\nconsumed 3\nwriter canonical\n",
        "operation sequence\nname w\nparam step = bits 3\nbytes 0D\nexpect value = 5\nconsumed 3\nwriter canonical\n",
        "the canonical writer's bytes",
    ),
    (
        "operation string\nname w\nparam buffer_size = 4\nbytes 01 00\nexpect refused embedded_nul\n",
        "operation string\nname w\nparam buffer_size = 4\nbytes 01 00\nexpect refused invalid_utf8\n",
        "the stated refusal reason",
    ),
    (
        "operation sequence\nname w\nparam step = bits 8\nparam step = align\nparam step = bits 8\nbytes 01 02\nexpect value = 1 | - | 2\nconsumed 16\nmeasure_at_least 23\n",
        "operation sequence\nname w\nparam step = bits 8\nparam step = align\nparam step = bits 8\nbytes 01 02\nexpect value = 1 | - | 2\nconsumed 16\nmeasure_at_least 24\n",
        "the measure floor",
    ),
    (
        "operation compressed_float\nname w\nparam min = 0\nparam max = 10\nparam res = 0.01\nwriter_input bits = 0x3CCCCCCD\nbytes 03 00\nexpect bits = 0x3CF5C290\nconsumed 10\n",
        "operation compressed_float\nname w\nparam min = 0\nparam max = 10\nparam res = 0.01\nwriter_input bits = 0x3CA3D70A\nbytes 03 00\nexpect bits = 0x3CF5C290\nconsumed 10\n",
        "the writer_input bytes",
    ),
    (
        "operation sequence\nname w\nparam step = bits 3\nparam step = end\nbytes 05\nexpect value = 5 | -\nconsumed 8\n",
        "operation sequence\nname w\nparam step = bits 3\nparam step = end\nbytes 05\nexpect value = 6 | -\nconsumed 8\n",
        "a sequence's values",
    ),
]


class ConformanceTest(unittest.TestCase):
    def test_corpus(self) -> None:
        run = run_corpus()
        lines = [f"conformance: corpus root {CORPUS_ROOT}"]
        for file, n, accepted, refused in run.counts:
            lines.append(f"  {file}: {n} records, {accepted} accepted, {refused} refused")
        for name, err in run.failed:
            lines.append(f"  FAIL {name}: {err}")
        lines.append(f"conformance: {run.total - len(run.failed)} of {run.total} vectors passed in {len(run.counts)} files")
        print("\n".join(lines))
        self.assertGreater(run.total, 0)
        self.assertEqual(run.failed, [], f"{len(run.failed)} of {run.total} vectors failed")

    def test_control_unknown_operation(self) -> None:
        recs = parse_vectors(os.path.join(CORPUS_ROOT, "conformance-controls", "unknown-operation.txt"))
        self.assertEqual(len(recs), 1)
        with self.assertRaisesRegex(RunnerError, "no runner for operation no_such_operation"):
            run_record(recs[0])

    def test_controls(self) -> None:
        names = {c.name for c in CONTROLS}
        listed = sorted(
            f[: -len(".expected")]
            for f in os.listdir(os.path.join(CORPUS_ROOT, "conformance-controls"))
            if f.endswith(".expected")
        )
        for n in listed:
            self.assertIn(n, names, f"conformance-controls/{n}.expected has no sabotage control in this runner")
        for c in CONTROLS:
            self.assertIn(c.name, listed, f"control {c.name} has no .expected list")
        for c in CONTROLS:
            with self.subTest(control=c.name):
                problems = run_control(c)
                self.assertEqual(problems, [], f"control {c.name} ({c.rule}):\n  " + "\n  ".join(problems))
                print(f"control {c.name}: exactly the {len(read_expected(c.name))} expected vectors failed")

    def test_control_needle_must_match(self) -> None:
        bogus = Control("no-such-needle", "none", [_in((READ,), "this text appears nowhere in the source", "")])
        with self.assertRaisesRegex(RunnerError, "needle no longer matches"):
            load_sabotaged(bogus)

    def test_runner_witnesses(self) -> None:
        for good, bad, why in WITNESSES:
            with self.subTest(witness=why):
                run_record(parse_vectors_text(good, "witness")[0])
                with self.assertRaises(RunnerError):
                    run_record(parse_vectors_text(bad, "witness")[0])
        for text in (
            "operation bool\nname m\nbytes\nexpect refused\n",
            "operation bool\nname m\nbytes\nexpect refused overflow\n",
            "operation bool\nname m\nbytes\nexpect refused past_end\nfrobnicate 1\n",
        ):
            with self.assertRaises(RunnerError):
                parse_vectors_text(text, "malformed")
        parse_vectors_text("operation bool\nname m\nbytes\nexpect refused past_end\n", "ok")

    def test_corpus_root_override(self) -> None:
        self.assertEqual(corpus_root_of(None), ROOT)
        self.assertEqual(corpus_root_of(""), ROOT)
        self.assertEqual(corpus_root_of("x"), "x")
        with self.assertRaisesRegex(RunnerError, "is missing or empty"):
            run_corpus(os.path.join(ROOT, "no-such-corpus-root"))


if __name__ == "__main__":
    unittest.main()
