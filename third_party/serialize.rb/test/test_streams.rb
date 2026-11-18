# frozen_string_literal: true

# The uniform behaviour rules (IMPLEMENTING.md U1 to U16) and the streams.
#
# No corpus vector can observe these: write misuse, measure behaviour, read
# misuse and the API's shape. Each test names the rule it proves.

require 'serialize'

module StreamsTest
  include Serialize

  R = Serialize::Ref
  WM = Serialize::WriteMisuse

  # one value and one operation per kind, each at an edge: the 64-bit split, a
  # degenerate range, the full range, a float's exact bits, an aligning op
  OPERATIONS = [
    ['bits 1', 1, ->(s, r) { s.serialize_bits(r, 1) }],
    ['bits 33', (1 << 33) - 1, ->(s, r) { s.serialize_bits(r, 33) }],
    ['bits 64', (1 << 64) - 1, ->(s, r) { s.serialize_bits(r, 64) }],
    ['uint8', 0xAB, ->(s, r) { s.serialize_uint8(r) }],
    ['uint16', 0xABCD, ->(s, r) { s.serialize_uint16(r) }],
    ['uint32', 0x89ABCDEF, ->(s, r) { s.serialize_uint32(r) }],
    ['uint64', 0x0123456789ABCDEF, ->(s, r) { s.serialize_uint64(r) }],
    ['bool', true, ->(s, r) { s.serialize_bool(r) }],
    ['int', -123_456_789, ->(s, r) { s.serialize_int(r, -(1 << 31), (1 << 31) - 1) }],
    ['int degenerate', 7, ->(s, r) { s.serialize_int(r, 7, 7) }],
    ['int64', (1 << 63) - 2, ->(s, r) { s.serialize_int64(r, -(1 << 63), (1 << 63) - 1) }],
    ['int128', -(1 << 127) + 3, ->(s, r) { s.serialize_int128(r, -(1 << 127), (1 << 127) - 1) }],
    ['uint128', (1 << 128) - 2, ->(s, r) { s.serialize_uint128(r) }],
    ['fixed q64.64', 1_512_366_075_204_170_930_115_394_234_220_888_865,
     ->(s, r) { s.serialize_fixed(r, 64, 64, -(1 << 63), (1 << 63) - 1) }],
    ['fixed q8.8', -832, ->(s, r) { s.serialize_fixed(r, 8, 8, -100, 100) }],
    ['int_relative one', 101, ->(s, r) { s.serialize_int_relative(100, r) }],
    ['int_relative bounded', 4000, ->(s, r) { s.serialize_int_relative(100, r) }],
    ['int_relative absolute', 2_100_000, ->(s, r) { s.serialize_int_relative(100, r) }],
    ['float', Serialize.float32_from_bits(0x40490FDA), ->(s, r) { s.serialize_float(r) }],
    ['float -0', -0.0, ->(s, r) { s.serialize_float(r) }],
    ['float subnormal', Serialize.float32_from_bits(1), ->(s, r) { s.serialize_float(r) }],
    ['double', 1.0 / 3.0, ->(s, r) { s.serialize_double(r) }],
    ['compressed_float', 5.0, ->(s, r) { s.serialize_compressed_float(r, 0.0, 10.0, 0.01) }],
    ['bytes', "\xDE\xAD\xBE\xEF".b, ->(s, r) { s.serialize_bytes(r, 4) }],
    ['string', '碁石', ->(s, r) { s.serialize_string(r, 8) }]
  ].freeze

  def self.write(ops, prefix = 0)
    ws = WriteStream.new(1024)
    ws.serialize_bits(R.new(prefix < 64 ? (1 << prefix) - 1 : 0), prefix) if prefix.positive?
    ops.each { |value, op| raise 'write failed' unless op.call(ws, R.new(value)) }
    ws.flush
    ws.data
  end
end

class RoundTripTest < Harness::TestCase
  include StreamsTest

  def test_every_operation_from_every_start_offset
    # each operation written after a prefix of every length from 0 to 63
    # bits, so it starts at every position in a 64-bit word, then read back
    OPERATIONS.each do |name, value, op|
      64.times do |prefix|
        subtest("#{name} at #{prefix}") do
          data = StreamsTest.write([[value, op]], prefix)
          rs = ReadStream.new(data)
          assert(rs.serialize_bits(R.new(0), prefix)) if prefix.positive?
          got = R.new
          assert(op.call(rs, got), rs.refusal.inspect)
          if value.is_a?(Float)
            want = name.include?('double') ? value : Serialize.float32_round(value)
            assert_equal(Serialize.float64_bits(want), Serialize.float64_bits(got.value))
          else
            assert_equal(value, got.value)
          end
          assert(rs.serialize_end, rs.refusal.inspect)
        end
      end
    end
  end

  def test_the_wire_is_little_endian_lsb_first
    assert_equal("\x05".b, StreamsTest.write([[5, ->(s, r) { s.serialize_bits(r, 3) }]]))
    assert_equal("\xEF\xCD\xAB\x89".b, StreamsTest.write([[0x89ABCDEF, ->(s, r) { s.serialize_uint32(r) }]]))
    assert_equal("\x01\x02".b, StreamsTest.write([[1, ->(s, r) { s.serialize_bits(r, 1) }],
                                                  [0x100, ->(s, r) { s.serialize_bits(r, 9) }]]))
    data = StreamsTest.write([['ab', ->(s, r) { s.serialize_string(r, 8) }]])
    assert_equal("\x02ab".b, data)
    assert_equal(Encoding::BINARY, data.encoding)
  end

  def test_measure_bounds_every_start_offset
    ms = MeasureStream.new
    OPERATIONS.each { |_, value, op| assert(op.call(ms, R.new(value))) }
    64.times do |prefix|
      ws = WriteStream.new(1024)
      ws.serialize_bits(R.new(0), prefix) if prefix.positive?
      OPERATIONS.each { |_, value, op| op.call(ws, R.new(value)) }
      assert_operator(ms.bits_processed, :>=, ws.bits_processed - prefix)
    end
  end

  def test_precomputed_compressed_float_is_wire_identical
    miv, n, delta = Serialize.compressed_float_params(-3.0, 7.5, 0.001)
    [-3.0, -2.9995, 0.0, 1.2345, 7.5, 100.0].each do |v|
      a = WriteStream.new(8)
      b = WriteStream.new(8)
      assert(a.serialize_compressed_float(R.new(v), -3.0, 7.5, 0.001))
      assert(b.serialize_compressed_float_precomputed(R.new(v), miv, n, delta, -3.0))
      a.flush
      b.flush
      assert_equal(a.data, b.data)
      assert_equal(a.bits_processed, b.bits_processed)
    end
  end

  def test_bits_required
    assert_equal(3, Serialize.bits_required(0, 7))
    assert_equal(4, Serialize.bits_required(0, 8))
    assert_equal(0, Serialize.bits_required(5, 5))
    assert_equal(32, Serialize.bits_required(-(1 << 31), (1 << 31) - 1))
  end

  def test_reads_return_binary_bytes_and_utf8_text
    data = StreamsTest.write([["\x00\xFF".b, ->(s, r) { s.serialize_bytes(r, 2) }],
                              ['é', ->(s, r) { s.serialize_string(r, 4) }]])
    rs = ReadStream.new(data)
    bytes = R.new
    text = R.new
    assert(rs.serialize_bytes(bytes, 2) && rs.serialize_string(text, 4) && rs.serialize_end)
    assert_equal(Encoding::BINARY, bytes.value.encoding)
    assert_equal(Encoding::UTF_8, text.value.encoding)
    assert_equal('é', text.value)
  end
end

class WriteMisuseTest < Harness::TestCase
  include StreamsTest

  def test_u1_write_buffer_is_a_multiple_of_8_bytes
    [1, 4, 12, -8, 8.0, nil].each do |capacity|
      subtest(capacity.inspect) { assert_raises(WM) { WriteStream.new(capacity) } }
    end
    WriteStream.new(0)
    WriteStream.new(16)
  end

  def test_write_misuse_is_an_argument_error
    assert(WM < ArgumentError)
  end

  def test_u3_u4_text_misuse_traps_on_write_and_measure
    [WriteStream.new(64), MeasureStream.new].each do |s|
      ["a\xFFb", "a\xED\xA0\x80b", "a\u0000b", 'x' * 8, 'ab'.b, 'ab'.encode('UTF-16LE'), :ab].each do |text|
        subtest("#{s.class.name} #{text.inspect}") { assert_raises(WM) { s.serialize_string(R.new(text), 8) } }
      end
      assert_equal(0, s.bits_processed)
      assert(s.serialize_string(R.new('ab'.encode('US-ASCII')), 8))
    end
  end

  def test_u5_int_bounds_are_int32
    [[-(1 << 31) - 1, 0], [0, 1 << 31], [5, 4], [0.0, 1]].each do |lo, hi|
      subtest("#{lo} #{hi}") do
        assert_raises(WM) { WriteStream.new(8).serialize_int(R.new(0), lo, hi) }
        assert_raises(WM) { MeasureStream.new.serialize_int(R.new(0), lo, hi) }
        rs = ReadStream.new("\x00".b * 8)
        refute(rs.serialize_int(R.new(0), lo, hi))
        assert_equal(Refusal::VALUE_OUT_OF_RANGE, rs.refusal)
      end
    end
  end

  def test_u8_over_wide_values_trap
    cases = [
      ->(s) { s.serialize_bits(R.new(8), 3) },
      ->(s) { s.serialize_bits(R.new(-1), 8) },
      ->(s) { s.serialize_bits(R.new(0), 0) },
      ->(s) { s.serialize_bits(R.new(0), 65) },
      ->(s) { s.serialize_bits(R.new(1.0), 8) },
      ->(s) { s.serialize_uint8(R.new(256)) },
      ->(s) { s.serialize_uint128(R.new(-1)) },
      ->(s) { s.serialize_uint128(R.new(1 << 128)) },
      ->(s) { s.serialize_int(R.new(8), 0, 7) },
      ->(s) { s.serialize_int64(R.new(1 << 63), -(1 << 63), (1 << 63) - 1) },
      ->(s) { s.serialize_int128(R.new(1 << 127), 0, (1 << 127) - 1) },
      ->(s) { s.serialize_fixed(R.new(25_601), 8, 8, -100, 100) },
      ->(s) { s.serialize_fixed(R.new(0), 8, 8, -200, 100) },
      ->(s) { s.serialize_fixed(R.new(0), 8, 7, 0, 1) },
      ->(s) { s.serialize_int_relative(5, R.new(5)) },
      ->(s) { s.serialize_int_relative(-1, R.new(5)) },
      ->(s) { s.serialize_int_relative(0, R.new(1 << 31)) },
      ->(s) { s.serialize_bool(R.new(1)) },
      ->(s) { s.serialize_bool(R.new(nil)) },
      ->(s) { s.serialize_bytes(R.new('ab'), 3) },
      ->(s) { s.serialize_bytes(R.new(''), -1) },
      ->(s) { s.serialize_bytes(R.new([1, 2]), 2) },
      ->(s) { s.serialize_string(R.new(''), 0) },
      ->(s) { s.serialize_string(R.new(''), (1 << 31) + 1) }
    ]
    cases.each_with_index do |c, i|
      [WriteStream.new(64), MeasureStream.new].each do |s|
        subtest("case #{i} #{s.class.name}") do
          assert_raises(WM) { c.call(s) }
          assert_equal(0, s.bits_processed)
        end
      end
    end
  end

  def test_finite_floats_only_on_write
    # [value, whether a double refuses it too]
    [[Float::NAN, true], [Float::INFINITY, true], [-Float::INFINITY, true], [1e39, false],
     [1 << 1100, true]].each do |v, double_too|
      [WriteStream.new(64), MeasureStream.new].each do |s|
        subtest("#{v} #{s.class.name}") do
          assert_raises(WM) { s.serialize_float(R.new(v)) }
          assert_raises(WM) { s.serialize_compressed_float(R.new(v), 0.0, 10.0, 0.01) }
          assert_raises(WM) { s.serialize_double(R.new(v)) } if double_too
          assert(s.serialize_double(R.new(v))) unless double_too
        end
      end
    end
    # a declaration whose top, min + delta, rounds to an infinity
    top = Serialize.float32_from_bits(0x7F7FFFFF)
    assert_raises(WM) { WriteStream.new(8).serialize_compressed_float(R.new(top), 3.0 * (2.0**103), top, 2.0**104) }
    [[1.0, 1.0, 0.1], [0.0, 1.0, 0.0], [0.0, 1.0, -1.0], [2.0, 1.0, 0.1], [0.0, Float::NAN, 0.1]].each do |bad|
      subtest(bad.inspect) { assert_raises(WM) { WriteStream.new(8).serialize_compressed_float(R.new(0.5), *bad) } }
    end
  end

  def test_u9_write_misuse_never_latches
    ws = WriteStream.new(8)
    assert_raises(WM) { ws.serialize_int(R.new(9), 0, 7) }
    assert_equal(0, ws.bits_processed)
    assert(ws.serialize_int(R.new(5), 0, 7))
    ws.flush
    assert_equal("\x05".b, ws.data)
  end

  def test_u9_reject_on_write_and_measure_traps_and_fail_records_nothing
    [WriteStream.new(8), MeasureStream.new].each do |s|
      assert_raises(WM) { s.reject }
      refute(s.fail)
      refute(s.fail(Refusal::PAST_END))
      refute(s.serialize_object { |_| false })
      assert(s.serialize_object { |_| true })
      assert(s.serialize_bits(R.new(1), 1))
    end
  end

  def test_u10_room_is_checked_for_the_whole_operation
    ws = WriteStream.new(8)
    ws.serialize_bits(R.new(0), 3)
    assert_raises(WM) { ws.serialize_string(R.new('abcdefgh'), 16) } # 4 + 1 + 64 bits, 61 left
    assert_raises(WM) { ws.serialize_bytes(R.new("\x00".b * 8), 8) }
    assert_raises(WM) { ws.serialize_bits(R.new(0), 62) }
    assert_equal(3, ws.bits_processed)
    assert_equal(61, ws.bits_available)
    assert(ws.serialize_bits(R.new(0), 61))
    assert_equal(0, ws.bits_available)
  end

  def test_u12_end_on_write_and_measure
    assert(WriteStream.new(8).serialize_end)
    assert(MeasureStream.new.serialize_end)
  end

  def test_u6_measure_charges_seven_bits_per_align
    ms = MeasureStream.new
    ms.serialize_bits(R.new(1), 8)
    ms.serialize_align
    ms.serialize_bits(R.new(1), 8)
    assert_equal(23, ms.bits_processed)
    assert_equal(7, ms.align_bits)
  end
end

class ReadMisuseTest < Harness::TestCase
  include StreamsTest

  def refuses(data = "\xFF".b * 32)
    rs = ReadStream.new(data)
    refute(yield(rs))
    assert_equal(Refusal::VALUE_OUT_OF_RANGE, rs.refusal)
    assert_equal(0, rs.bits_processed)
  end

  def test_u7_u16_read_misuse_refuses_and_never_raises
    cases = [
      ->(s) { s.serialize_bits(R.new(0), 0) },
      ->(s) { s.serialize_bits(R.new(0), 65) },
      ->(s) { s.serialize_bits(R.new(0), 8.0) },
      ->(s) { s.serialize_int(R.new(0), 7, 0) },
      ->(s) { s.serialize_int64(R.new(0), 0, 1 << 63) },
      ->(s) { s.serialize_int128(R.new(0), 1, 0) },
      ->(s) { s.serialize_fixed(R.new(0), 8, 7, 0, 1) },
      ->(s) { s.serialize_fixed(R.new(0), 8, 8, -200, 100) },
      ->(s) { s.serialize_int_relative(-1, R.new(0)) },
      ->(s) { s.serialize_int_relative(1 << 31, R.new(0)) },
      ->(s) { s.serialize_compressed_float(R.new(0.0), 1.0, 0.0, 0.1) },
      ->(s) { s.serialize_compressed_float(R.new(0.0), 0.0, 1.0, 0.0) },
      ->(s) { s.serialize_compressed_float(R.new(0.0), 0.0, 1.0, nil) },
      ->(s) { s.serialize_compressed_float_precomputed(R.new(0.0), 1023, 9, 10.0, 0.0) },
      ->(s) { s.serialize_compressed_float_precomputed(R.new(0.0), 0, 0, 10.0, 0.0) },
      ->(s) { s.serialize_bytes(R.new(''), -1) },
      ->(s) { s.serialize_string(R.new(''), 0) },
      ->(s) { s.serialize_string(R.new(''), -5) }
    ]
    cases.each_with_index do |c, i|
      subtest("case #{i}") { refuses { |rs| c.call(rs) } }
    end
  end

  def test_u7_a_read_stream_over_no_bytes_refuses
    rs = ReadStream.new([1, 2])
    assert_equal(Refusal::VALUE_OUT_OF_RANGE, rs.refusal)
    refute(rs.serialize_end)
  end

  def test_reads_of_hostile_bytes_never_raise
    ['', "\xFF", "\xFF" * 3, "\x00" * 17, (0..255).to_a.pack('C*'), "\xED\xA0\x80" * 4].each do |data|
      OPERATIONS.each do |name, _, op|
        subtest("#{name} #{data.bytesize}") do
          rs = ReadStream.new(data)
          op.call(rs, R.new)
          rs.serialize_end
        end
      end
    end
  end

  def test_the_reader_copies_its_bytes
    data = +"\x05"
    rs = ReadStream.new(data)
    data.replace("\x07")
    got = R.new
    assert(rs.serialize_bits(got, 3))
    assert_equal(5, got.value)
  end
end

class RefusalTest < Harness::TestCase
  include StreamsTest

  def test_u14_refusal_names
    assert_equal('none', Serialize.refusal_name(nil))
    assert_equal('past_end', Serialize.refusal_name(Refusal::PAST_END))
    assert_equal('', Serialize.refusal_name(:overflow))
    assert_equal(9, Serialize::REASONS.size)
    [nil, :overflow, 'past_end'].each do |reason|
      rs = ReadStream.new("\x00")
      refute(rs.fail(reason))
      assert_equal(Refusal::REJECTED, rs.refusal)
    end
    rs = ReadStream.new("\x00")
    refute(rs.fail(Refusal::TRAILING_DATA))
    assert_equal(Refusal::TRAILING_DATA, rs.refusal)
  end

  def test_u2_reject_latches_rejected_and_the_first_reason_wins
    rs = ReadStream.new("\x01")
    assert(rs.serialize_bits(R.new(0), 1))
    refute(rs.reject)
    assert_equal(Refusal::REJECTED, rs.refusal)
    refute(rs.serialize_bits(R.new(0), 1))
    assert_equal(Refusal::REJECTED, rs.refusal)
    rs = ReadStream.new('')
    refute(rs.serialize_bits(R.new(0), 1))
    refute(rs.reject)
    assert_equal(Refusal::PAST_END, rs.refusal)
  end

  def test_u2_an_object_that_refuses_on_its_own_latches_rejected
    rs = ReadStream.new("\x01")
    refute(rs.serialize_object { |_| false })
    assert_equal(Refusal::REJECTED, rs.refusal)
    rs = ReadStream.new("\x01")
    refute(rs.serialize_object { |_| 1 })
    assert_equal(Refusal::REJECTED, rs.refusal)
  end

  def test_a_refused_read_leaves_its_destination_unwritten
    sentinel = Object.new
    ref = R.new(sentinel)
    rs = ReadStream.new("\x0F")
    refute(rs.serialize_int(ref, 0, 8))
    assert_same(sentinel, ref.value)
    assert_equal(Refusal::VALUE_OUT_OF_RANGE, rs.refusal)
    assert_equal(0, rs.bits_remaining)
  end
end

class CopyStringTest < Harness::TestCase
  include StreamsTest

  def test_u15_copy_string
    assert_equal('golden', Serialize.copy_string('golden', 16))
    assert_equal('gol', Serialize.copy_string('golden', 4))
    assert_equal('ab', Serialize.copy_string("ab\u0000cd", 16))
    assert_equal('碁', Serialize.copy_string('碁石', 6)) # 5 bytes fit, 3 make a whole character
    assert_equal('碁石', Serialize.copy_string('碁石', 7))
    assert_equal('', Serialize.copy_string('x', 1))
    assert_equal(Encoding::UTF_8, Serialize.copy_string('x', 1).encoding)
    assert_raises(WM) { Serialize.copy_string('x', 0) }
    assert_raises(WM) { Serialize.copy_string("a\xFF", 8) }
    ["\u{1F600}\u{1F600}", 'é', 'abc'].each do |text|
      (1...10).each do |size|
        subtest("#{text} #{size}") do
          assert(WriteStream.new(64).serialize_string(R.new(Serialize.copy_string(text, size)), size))
        end
      end
    end
  end
end

class FloatTest < Harness::TestCase
  include StreamsTest

  def test_float32_round_overflow_and_ties
    assert_equal(Float::INFINITY, Serialize.float32_round(1e39))
    assert_equal(-Float::INFINITY, Serialize.float32_round(-1e39))
    assert_equal(0x3F800000, Serialize.float32_bits(1.0 + (2.0**-24))) # a tie rounds to even
    assert_equal(0x3F800002, Serialize.float32_bits(1.0 + (3 * (2.0**-24))))
  end

  def test_float32_round_at_the_largest_float32
    # IEEE rounding: above the largest float32 and below the halfway point to
    # 2^128 the value rounds to the largest float32; the halfway point is a
    # tie, which rounds to even, the infinity
    max = Serialize::FLOAT32_MAX
    half = Serialize::FLOAT32_OVERFLOW
    assert_equal(0x7F7FFFFF, Serialize.float32_bits(max))
    assert_equal(0x7F7FFFFF, Serialize.float32_bits(max.next_float))
    assert_equal(0x7F7FFFFF, Serialize.float32_bits(half.prev_float))
    assert_equal(0xFF7FFFFF, Serialize.float32_bits(-half.prev_float))
    assert_equal(0x7F800000, Serialize.float32_bits(half))
    assert_equal(0xFF800000, Serialize.float32_bits(-half))
    ws = WriteStream.new(8)
    assert(ws.serialize_float(R.new(half.prev_float)))
    ws.flush
    assert_equal("\xFF\xFF\x7F\x7F".b, ws.data)
  end

  def test_compressed_float_rounds_twice_on_write
    # STANDARD.md, "compressed_float": over [0, 10] at 0.01 a conforming
    # writer writes 1, 3, 11 and 1000 for these inputs
    [[0.005, 1], [0.025, 3], [0.105, 11], [9.995, 1000]].each do |value, want|
      ws = WriteStream.new(8)
      ws.serialize_compressed_float(R.new(Serialize.float32_round(value)), 0.0, 10.0, 0.01)
      ws.flush
      assert_equal(want, ws.data.unpack1('v'), value.to_s)
    end
  end
end
