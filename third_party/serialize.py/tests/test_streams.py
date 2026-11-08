"""The uniform behaviour rules (IMPLEMENTING.md U1 to U16) and the streams.

No corpus vector can observe these: write misuse, measure behaviour, read
misuse and the API's shape. Each test names the rule it proves.
"""

from __future__ import annotations

import math
import unittest
from typing import Any, Callable

from serialize import (
    MeasureStream,
    ReadStream,
    Ref,
    Refusal,
    Stream,
    WriteMisuse,
    WriteStream,
    bits_required,
    compressed_float_params,
    copy_string,
    float32_bits,
    float32_from_bits,
    float32_round,
    float64_bits,
    refusal_name,
)

Op = Callable[[Stream, Ref[Any]], bool]

# one value and one operation per kind, each at an edge: the 64-bit split, a
# degenerate range, the full range, a float's exact bits, an aligning op
OPERATIONS: list[tuple[str, Any, Op]] = [
    ("bits 1", 1, lambda s, r: s.serialize_bits(r, 1)),
    ("bits 33", (1 << 33) - 1, lambda s, r: s.serialize_bits(r, 33)),
    ("bits 64", (1 << 64) - 1, lambda s, r: s.serialize_bits(r, 64)),
    ("uint8", 0xAB, lambda s, r: s.serialize_uint8(r)),
    ("uint64", 0x0123456789ABCDEF, lambda s, r: s.serialize_uint64(r)),
    ("bool", True, lambda s, r: s.serialize_bool(r)),
    ("int", -123456789, lambda s, r: s.serialize_int(r, -(1 << 31), (1 << 31) - 1)),
    ("int degenerate", 7, lambda s, r: s.serialize_int(r, 7, 7)),
    ("int64", (1 << 63) - 2, lambda s, r: s.serialize_int64(r, -(1 << 63), (1 << 63) - 1)),
    ("int128", -(1 << 127) + 3, lambda s, r: s.serialize_int128(r, -(1 << 127), (1 << 127) - 1)),
    ("uint128", (1 << 128) - 2, lambda s, r: s.serialize_uint128(r)),
    ("fixed q64.64", 1512366075204170930115394234220888865, lambda s, r: s.serialize_fixed(r, 64, 64, -(1 << 63), (1 << 63) - 1)),
    ("fixed q8.8", -832, lambda s, r: s.serialize_fixed(r, 8, 8, -100, 100)),
    ("int_relative one", 101, lambda s, r: s.serialize_int_relative(100, r)),
    ("int_relative absolute", 2100000, lambda s, r: s.serialize_int_relative(100, r)),
    ("float", float32_from_bits(0x40490FDA), lambda s, r: s.serialize_float(r)),
    ("float -0", -0.0, lambda s, r: s.serialize_float(r)),
    ("float subnormal", float32_from_bits(1), lambda s, r: s.serialize_float(r)),
    ("double", 1.0 / 3.0, lambda s, r: s.serialize_double(r)),
    ("compressed_float", 5.0, lambda s, r: s.serialize_compressed_float(r, 0.0, 10.0, 0.01)),
    ("bytes", b"\xde\xad\xbe\xef", lambda s, r: s.serialize_bytes(r, 4)),
    ("string", "碁石", lambda s, r: s.serialize_string(r, 8)),
]


def write(ops: list[tuple[Any, Op]], prefix: int = 0) -> bytes:
    ws = WriteStream(1024)
    if prefix:
        ws.serialize_bits(Ref((1 << prefix) - 1 if prefix < 64 else 0), prefix)
    for value, op in ops:
        assert op(ws, Ref(value))
    ws.flush()
    return ws.data()


def bits(value: float) -> int:
    return float64_bits(value)


class RoundTripTest(unittest.TestCase):
    def test_every_operation_from_every_start_offset(self) -> None:
        # each operation written after a prefix of every length from 0 to 63
        # bits, so it starts at every position in a 64-bit word, then read back
        for name, value, op in OPERATIONS:
            for prefix in range(64):
                with self.subTest(op=name, prefix=prefix):
                    data = write([(value, op)], prefix)
                    rs = ReadStream(data)
                    if prefix:
                        self.assertTrue(rs.serialize_bits(Ref(0), prefix))
                    got: Ref[Any] = Ref(None)
                    self.assertTrue(op(rs, got), rs.refusal())
                    if isinstance(value, float):
                        self.assertEqual(bits(got.value), bits(float32_round(value) if "double" not in name else value))
                    else:
                        self.assertEqual(got.value, value)
                    self.assertTrue(rs.serialize_end(), rs.refusal())

    def test_measure_bounds_every_start_offset(self) -> None:
        ms = MeasureStream()
        for _, value, op in OPERATIONS:
            self.assertTrue(op(ms, Ref(value)))
        for prefix in range(64):
            ws = WriteStream(1024)
            if prefix:
                ws.serialize_bits(Ref(0), prefix)
            for _, value, op in OPERATIONS:
                op(ws, Ref(value))
            self.assertGreaterEqual(ms.bits_processed(), ws.bits_processed() - prefix)

    def test_precomputed_compressed_float_is_wire_identical(self) -> None:
        params = compressed_float_params(-3.0, 7.5, 0.001)
        assert params is not None
        miv, n, delta = params
        for v in (-3.0, -2.9995, 0.0, 1.2345, 7.5, 100.0):
            a = WriteStream(8)
            b = WriteStream(8)
            self.assertTrue(a.serialize_compressed_float(Ref(v), -3.0, 7.5, 0.001))
            self.assertTrue(b.serialize_compressed_float_precomputed(Ref(v), miv, n, delta, -3.0))
            a.flush()
            b.flush()
            self.assertEqual(a.data(), b.data())
            self.assertEqual(a.bits_processed(), b.bits_processed())

    def test_writer_pads_the_final_byte_with_zero(self) -> None:
        self.assertEqual(write([(5, lambda s, r: s.serialize_bits(r, 3))]), b"\x05")

    def test_bits_required(self) -> None:
        self.assertEqual(bits_required(0, 7), 3)
        self.assertEqual(bits_required(0, 8), 4)
        self.assertEqual(bits_required(5, 5), 0)
        self.assertEqual(bits_required(-(1 << 31), (1 << 31) - 1), 32)


class WriteMisuseTest(unittest.TestCase):
    def test_u1_write_buffer_is_a_multiple_of_8_bytes(self) -> None:
        for capacity in (1, 4, 12, -8):
            with self.assertRaises(WriteMisuse):
                WriteStream(capacity)
        WriteStream(0)
        WriteStream(16)

    def test_u3_u4_text_misuse_traps_on_write_and_measure(self) -> None:
        for s in (WriteStream(64), MeasureStream()):
            for text in ("a\ud800b", "a\x00b", "x" * 8):
                with self.subTest(stream=type(s).__name__, text=text):
                    with self.assertRaises(WriteMisuse):
                        s.serialize_string(Ref(text), 8)
            self.assertEqual(s.bits_processed(), 0)

    def test_u5_int_bounds_are_int32(self) -> None:
        for lo, hi in ((-(1 << 31) - 1, 0), (0, 1 << 31), (5, 4)):
            with self.subTest(lo=lo, hi=hi):
                with self.assertRaises(WriteMisuse):
                    WriteStream(8).serialize_int(Ref(0), lo, hi)
                with self.assertRaises(WriteMisuse):
                    MeasureStream().serialize_int(Ref(0), lo, hi)
                rs = ReadStream(b"\x00" * 8)
                self.assertFalse(rs.serialize_int(Ref(0), lo, hi))
                self.assertEqual(rs.refusal(), Refusal.VALUE_OUT_OF_RANGE)

    def test_u8_over_wide_values_trap(self) -> None:
        cases: list[Callable[[Stream], bool]] = [
            lambda s: s.serialize_bits(Ref(8), 3),
            lambda s: s.serialize_bits(Ref(-1), 8),
            lambda s: s.serialize_bits(Ref(0), 0),
            lambda s: s.serialize_bits(Ref(0), 65),
            lambda s: s.serialize_uint8(Ref(256)),
            lambda s: s.serialize_uint128(Ref(-1)),
            lambda s: s.serialize_uint128(Ref(1 << 128)),
            lambda s: s.serialize_int(Ref(8), 0, 7),
            lambda s: s.serialize_int64(Ref(1 << 63), -(1 << 63), (1 << 63) - 1),
            lambda s: s.serialize_int128(Ref(1 << 127), 0, (1 << 127) - 1),
            lambda s: s.serialize_fixed(Ref(25601), 8, 8, -100, 100),
            lambda s: s.serialize_fixed(Ref(0), 8, 8, -200, 100),
            lambda s: s.serialize_fixed(Ref(0), 8, 7, 0, 1),
            lambda s: s.serialize_int_relative(5, Ref(5)),
            lambda s: s.serialize_int_relative(-1, Ref(5)),
            lambda s: s.serialize_int_relative(0, Ref(1 << 31)),
            lambda s: s.serialize_bool(Ref(1)),  # type: ignore[arg-type]
            lambda s: s.serialize_bytes(Ref(b"ab"), 3),
            lambda s: s.serialize_bytes(Ref(b""), -1),
            lambda s: s.serialize_string(Ref(""), 0),
        ]
        for i, case in enumerate(cases):
            for s in (WriteStream(64), MeasureStream()):
                with self.subTest(case=i, stream=type(s).__name__):
                    with self.assertRaises(WriteMisuse):
                        case(s)

    def test_finite_floats_only_on_write(self) -> None:
        for v in (math.nan, math.inf, -math.inf, 1e39):
            for s in (WriteStream(64), MeasureStream()):
                with self.subTest(value=v, stream=type(s).__name__):
                    with self.assertRaises(WriteMisuse):
                        s.serialize_float(Ref(v))
                    with self.assertRaises(WriteMisuse):
                        s.serialize_compressed_float(Ref(v), 0.0, 10.0, 0.01)
                    if v != 1e39:
                        with self.assertRaises(WriteMisuse):
                            s.serialize_double(Ref(v))
        # a declaration whose top, min + delta, rounds to an infinity
        top = float32_from_bits(0x7F7FFFFF)
        with self.assertRaises(WriteMisuse):
            WriteStream(8).serialize_compressed_float(Ref(top), 3.0 * 2.0**103, top, 2.0**104)
        for bad in ((1.0, 1.0, 0.1), (0.0, 1.0, 0.0), (0.0, 1.0, -1.0), (2.0, 1.0, 0.1)):
            with self.assertRaises(WriteMisuse):
                WriteStream(8).serialize_compressed_float(Ref(0.5), *bad)

    def test_u9_write_misuse_never_latches(self) -> None:
        ws = WriteStream(8)
        with self.assertRaises(WriteMisuse):
            ws.serialize_int(Ref(9), 0, 7)
        self.assertEqual(ws.bits_processed(), 0)
        self.assertTrue(ws.serialize_int(Ref(5), 0, 7))
        ws.flush()
        self.assertEqual(ws.data(), b"\x05")

    def test_u9_reject_on_write_and_measure_traps_and_fail_records_nothing(self) -> None:
        for s in (WriteStream(8), MeasureStream()):
            with self.assertRaises(WriteMisuse):
                s.reject()
            self.assertFalse(s.fail())
            self.assertFalse(s.fail(Refusal.PAST_END))
            self.assertFalse(s.serialize_object(lambda _s: False))
            self.assertTrue(s.serialize_bits(Ref(1), 1))

    def test_u10_room_is_checked_for_the_whole_operation(self) -> None:
        ws = WriteStream(8)
        ws.serialize_bits(Ref(0), 3)
        with self.assertRaises(WriteMisuse):
            ws.serialize_string(Ref("abcdefgh"), 16)  # 4 + 1 + 64 bits, 61 left
        with self.assertRaises(WriteMisuse):
            ws.serialize_bytes(Ref(b"\x00" * 8), 8)
        with self.assertRaises(WriteMisuse):
            ws.serialize_bits(Ref(0), 62)
        self.assertEqual(ws.bits_processed(), 3)
        self.assertEqual(ws.bits_available(), 61)

    def test_u12_end_on_write_and_measure(self) -> None:
        self.assertTrue(WriteStream(8).serialize_end())
        self.assertTrue(MeasureStream().serialize_end())

    def test_u6_measure_charges_seven_bits_per_align(self) -> None:
        ms = MeasureStream()
        ms.serialize_bits(Ref(1), 8)
        ms.serialize_align()
        ms.serialize_bits(Ref(1), 8)
        self.assertEqual(ms.bits_processed(), 23)
        self.assertEqual(ms.align_bits(), 7)


class ReadMisuseTest(unittest.TestCase):
    def refuses(self, op: Callable[[ReadStream], bool], data: bytes = b"\xff" * 32) -> None:
        rs = ReadStream(data)
        self.assertFalse(op(rs))
        self.assertEqual(rs.refusal(), Refusal.VALUE_OUT_OF_RANGE)
        self.assertEqual(rs.bits_processed(), 0)

    def test_u7_u16_read_misuse_refuses_and_never_raises(self) -> None:
        cases: list[Callable[[ReadStream], bool]] = [
            lambda s: s.serialize_bits(Ref(0), 0),
            lambda s: s.serialize_bits(Ref(0), 65),
            lambda s: s.serialize_int(Ref(0), 7, 0),
            lambda s: s.serialize_int64(Ref(0), 0, 1 << 63),
            lambda s: s.serialize_int128(Ref(0), 1, 0),
            lambda s: s.serialize_fixed(Ref(0), 8, 7, 0, 1),
            lambda s: s.serialize_fixed(Ref(0), 8, 8, -200, 100),
            lambda s: s.serialize_int_relative(-1, Ref(0)),
            lambda s: s.serialize_int_relative(1 << 31, Ref(0)),
            lambda s: s.serialize_compressed_float(Ref(0.0), 1.0, 0.0, 0.1),
            lambda s: s.serialize_compressed_float(Ref(0.0), 0.0, 1.0, 0.0),
            lambda s: s.serialize_compressed_float_precomputed(Ref(0.0), 1023, 9, 10.0, 0.0),
            lambda s: s.serialize_compressed_float_precomputed(Ref(0.0), 0, 0, 10.0, 0.0),
            lambda s: s.serialize_bytes(Ref(b""), -1),
            lambda s: s.serialize_string(Ref(""), 0),
            lambda s: s.serialize_string(Ref(""), -5),
        ]
        for i, case in enumerate(cases):
            with self.subTest(case=i):
                self.refuses(case)

    def test_u7_a_read_stream_over_no_bytes_refuses(self) -> None:
        rs = ReadStream("not bytes")  # type: ignore[arg-type]
        self.assertEqual(rs.refusal(), Refusal.VALUE_OUT_OF_RANGE)
        self.assertFalse(rs.serialize_end())

    def test_reads_of_hostile_bytes_never_raise(self) -> None:
        for data in (b"", b"\xff", b"\xff" * 3, b"\x00" * 17, bytes(range(256))):
            for _, _, op in OPERATIONS:
                rs = ReadStream(data)
                op(rs, Ref(None))
                rs.serialize_end()


class RefusalTest(unittest.TestCase):
    def test_u14_refusal_names(self) -> None:
        self.assertEqual(refusal_name(None), "none")
        self.assertEqual(refusal_name("past_end"), "past_end")
        self.assertEqual(refusal_name("overflow"), "")
        for reason in (None, "overflow"):
            rs = ReadStream(b"\x00")
            self.assertFalse(rs.fail(reason))
            self.assertEqual(rs.refusal(), Refusal.REJECTED)
        rs = ReadStream(b"\x00")
        self.assertFalse(rs.fail(Refusal.TRAILING_DATA))
        self.assertEqual(rs.refusal(), Refusal.TRAILING_DATA)

    def test_u2_reject_latches_rejected_and_the_first_reason_wins(self) -> None:
        rs = ReadStream(b"\x01")
        self.assertTrue(rs.serialize_bits(Ref(0), 1))
        self.assertFalse(rs.reject())
        self.assertEqual(rs.refusal(), Refusal.REJECTED)
        self.assertFalse(rs.serialize_bits(Ref(0), 1))
        self.assertEqual(rs.refusal(), Refusal.REJECTED)
        rs = ReadStream(b"")
        self.assertFalse(rs.serialize_bits(Ref(0), 1))
        self.assertFalse(rs.reject())
        self.assertEqual(rs.refusal(), Refusal.PAST_END)

    def test_u2_an_object_that_refuses_on_its_own_latches_rejected(self) -> None:
        rs = ReadStream(b"\x01")
        self.assertFalse(rs.serialize_object(lambda s: False))
        self.assertEqual(rs.refusal(), Refusal.REJECTED)

    def test_a_refused_read_leaves_its_destination_unwritten(self) -> None:
        sentinel = object()
        ref: Ref[Any] = Ref(sentinel)
        rs = ReadStream(b"\x0f")
        self.assertFalse(rs.serialize_int(ref, 0, 8))
        self.assertIs(ref.value, sentinel)
        self.assertEqual(rs.refusal(), Refusal.VALUE_OUT_OF_RANGE)
        self.assertEqual(rs.bits_remaining(), 0)


class CopyStringTest(unittest.TestCase):
    def test_u15_copy_string(self) -> None:
        self.assertEqual(copy_string("golden", 16), "golden")
        self.assertEqual(copy_string("golden", 4), "gol")
        self.assertEqual(copy_string("ab\x00cd", 16), "ab")
        self.assertEqual(copy_string("碁石", 6), "碁")  # 5 bytes fit, 3 make a whole character
        self.assertEqual(copy_string("碁石", 7), "碁石")
        self.assertEqual(copy_string("x", 1), "")
        with self.assertRaises(WriteMisuse):
            copy_string("x", 0)
        with self.assertRaises(WriteMisuse):
            copy_string("a\ud800", 8)
        for text in ("\U0001f600\U0001f600", "é", "abc"):
            for size in range(1, 10):
                ws = WriteStream(64)
                self.assertTrue(ws.serialize_string(Ref(copy_string(text, size)), size))


class FloatTest(unittest.TestCase):
    def test_float32_round_overflow_and_ties(self) -> None:
        self.assertEqual(float32_round(1e39), math.inf)
        self.assertEqual(float32_round(-1e39), -math.inf)
        self.assertEqual(float32_bits(1.0 + 2.0**-24), 0x3F800000)  # a tie rounds to even
        self.assertEqual(float32_bits(1.0 + 3 * 2.0**-24), 0x3F800002)

    def test_compressed_float_rounds_twice_on_write(self) -> None:
        # STANDARD.md, "compressed_float": over [0, 10] at 0.01 a conforming
        # writer writes 1, 3, 11 and 1000 for these inputs
        for value, want in ((0.005, 1), (0.025, 3), (0.105, 11), (9.995, 1000)):
            ws = WriteStream(8)
            ws.serialize_compressed_float(Ref(float32_round(value)), 0.0, 10.0, 0.01)
            ws.flush()
            self.assertEqual(int.from_bytes(ws.data(), "little"), want, value)


if __name__ == "__main__":
    unittest.main()
