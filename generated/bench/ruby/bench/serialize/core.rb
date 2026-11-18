# frozen_string_literal: true

module Bench
  # The streams, the refusal list and the helpers of serialize.rb.
  #
  # The wire is STANDARD.md, format version 2.0. The shape of the API is the
  # family's: one serialize function per message, written once against the
  # Stream surface and run against a WriteStream, a ReadStream or a
  # MeasureStream. A scalar travels in a Ref, so the same call stores through it
  # on read and takes the value from it on write.
  #
  # Two mechanisms, never mixed (IMPLEMENTING.md U7, U9):
  #
  # * A READ refusal is a value. Every read returns false and latches the first
  #   refusal's reason, which ReadStream#refusal reports from the closed list in
  #   Refusal. A reader never raises on any input, and caller misuse on read (a
  #   bad declaration, a buffer size below 1) refuses as value_out_of_range.
  # * WRITE and MEASURE misuse raises WriteMisuse, an ArgumentError, at the call
  #   and before any bit of the operation is written. Ruby has no unchecked
  #   build, so this port is a checked build in every run. Misuse never latches:
  #   a write or a measure stream has no refusal state.
  #
  # The bit accumulator of a writer is an Integer of unbounded width: values are
  # ORed in above the bits already held, and each whole 64-bit word below is
  # appended to a binary String. Nothing is a fixed word buffer, a ring or a
  # reused slot, so the layout carries no index arithmetic beyond that append.
  module Serialize
    FORMAT_VERSION = '2.0'

    # A holder a serialize call stores through on read and reads from on write.
    #
    # A language necessity (IMPLEMENTING.md U15): a Ruby scalar cannot be passed
    # by reference, and the unified serialize function needs one.
    class Ref
      attr_accessor :value

      def initialize(value = nil)
        @value = value
      end

      def inspect
        "Ref(#{@value.inspect})"
      end
    end

    # The closed list of refusal reasons (STANDARD.md, "Refusal reasons").
    module Refusal
      PAST_END = :past_end
      VALUE_OUT_OF_RANGE = :value_out_of_range
      NONZERO_PADDING = :nonzero_padding
      TRAILING_DATA = :trailing_data
      NON_FINITE_FLOAT = :non_finite_float
      INVALID_UTF8 = :invalid_utf8
      EMBEDDED_NUL = :embedded_nul
      NON_CANONICAL = :non_canonical
      REJECTED = :rejected
    end

    PAST_END = Refusal::PAST_END
    VALUE_OUT_OF_RANGE = Refusal::VALUE_OUT_OF_RANGE
    NONZERO_PADDING = Refusal::NONZERO_PADDING
    TRAILING_DATA = Refusal::TRAILING_DATA
    NON_FINITE_FLOAT = Refusal::NON_FINITE_FLOAT
    INVALID_UTF8 = Refusal::INVALID_UTF8
    EMBEDDED_NUL = Refusal::EMBEDDED_NUL
    NON_CANONICAL = Refusal::NON_CANONICAL
    REJECTED = Refusal::REJECTED

    REASONS = [
      PAST_END, VALUE_OUT_OF_RANGE, NONZERO_PADDING, TRAILING_DATA, NON_FINITE_FLOAT,
      INVALID_UTF8, EMBEDDED_NUL, NON_CANONICAL, REJECTED
    ].freeze

    # Write or measure misuse (IMPLEMENTING.md U9): this port's trap.
    #
    # Raised at the call, before any bit of the operation is written, for a
    # value outside its range or bit width, a non-finite float, text that is not
    # valid UTF-8, holds a NUL or does not fit its buffer, a bad declaration, a
    # write buffer whose size is not a multiple of 8 bytes, an operation that
    # does not fit the room left, and reject on a writer or a measure. Never a
    # refusal: a write or a measure stream latches nothing.
    class WriteMisuse < ArgumentError; end

    INT32_MIN = -(1 << 31)
    INT32_MAX = (1 << 31) - 1
    INT64_MIN = -(1 << 63)
    INT64_MAX = (1 << 63) - 1
    INT128_MIN = -(1 << 127)
    INT128_MAX = (1 << 127) - 1
    UINT128_LIMIT = 1 << 128
    M64 = (1 << 64) - 1

    # int_relative (STANDARD.md, "int_relative"): the domain and the tiers.
    RELATIVE_MAX = INT32_MAX
    LAST_BOUNDED_MAX = 69_914
    # [flag count, low, high] for the bounded tiers, in ladder order
    TIERS = [[2, 2, 6], [3, 7, 23], [4, 24, 280], [5, 281, 4377], [6, 4378, 69_914]].freeze

    # the largest float32 below 2^32 (STANDARD.md, "compressed_float")
    COMPRESSED_FLOAT_VALUES_MAX = 4_294_967_040.0

    # the largest finite float32, and the float64 halfway between it and 2^128:
    # a value strictly between them rounds to it, and the tie rounds to even,
    # which is the infinity
    FLOAT32_MAX = 3.4028234663852886e+38
    FLOAT32_OVERFLOW = 3.4028235677973366e+38

    # the family spelling of a refusal (IMPLEMENTING.md U14): "none" for no
    # refusal, the reason's name for one of the closed list, and "" for anything
    # else, which names no reason
    def self.refusal_name(reason)
      return 'none' if reason.nil?

      REASONS.include?(reason) ? reason.to_s : ''
    end

    # The bits a ranged field over [min, max] costs: 0 for min == max. The bit
    # length of max - min, which is the family's 32 - count_leading_zeros(max -
    # min) at every width. min <= max.
    def self.bits_required(min, max)
      (max - min).bit_length
    end

    # A number a float parameter takes: a Float, or an Integer below 2^1023 in
    # magnitude, which converts to a finite Float.
    def self.number?(value)
      value.is_a?(Float) || (value.is_a?(Integer) && value.bit_length < 1024)
    end

    # value rounded to float32, round to nearest, ties to even.
    #
    # A language necessity (IMPLEMENTING.md U15): Ruby has no float32 type.
    # pack('e') narrows a finite value above the largest float32 to an infinity,
    # where IEEE rounding gives the largest float32 up to the halfway point; the
    # correction restores that. A value whose rounding overflows float32
    # returns the infinity of its sign. Every float32 operation of
    # compressed_float is the float64 operation then this rounding, which is
    # exact: a float64 result rounded to float32 is the correctly rounded
    # float32 result for +, -, * and / of float32 operands, because float64
    # carries more than 2 * 24 + 2 bits.
    def self.float32_round(value)
      narrowed = [value].pack('e').unpack1('e')
      return narrowed unless narrowed.infinite? && value.abs < FLOAT32_OVERFLOW

      value.negative? ? -FLOAT32_MAX : FLOAT32_MAX
    end

    # The 32 bits of value as a float32 (after float32_round).
    def self.float32_bits(value)
      [float32_round(value)].pack('e').unpack1('L<')
    end

    # The Float whose float32 pattern is bits. Exact for every finite pattern.
    def self.float32_from_bits(bits)
      [bits].pack('L<').unpack1('e')
    end

    # The 64 bits of value as a float64.
    def self.float64_bits(value)
      [value].pack('E').unpack1('Q<')
    end

    # The Float whose float64 pattern is bits.
    def self.float64_from_bits(bits)
      [bits].pack('Q<').unpack1('E')
    end

    # A float32 pattern whose exponent field is not all ones.
    def self.float_bits_finite?(bits)
      (bits & 0x7F800000) != 0x7F800000
    end

    # A float64 pattern whose exponent field is not all ones.
    def self.double_bits_finite?(bits)
      (bits & 0x7FF0000000000000) != 0x7FF0000000000000
    end

    # Text a string operation carries: a UTF-8 (or US-ASCII) String whose bytes
    # are well-formed UTF-8.
    def self.utf8_text?(value)
      value.is_a?(String) && TEXT_ENCODINGS.include?(value.encoding) && value.valid_encoding?
    end

    TEXT_ENCODINGS = [Encoding::UTF_8, Encoding::US_ASCII].freeze

    # ---------------------------------------------------------------------------
    # declarations: each returns nil for one that is not a declaration of the
    # format, so a reader can refuse it as a value and a writer can raise
    # ---------------------------------------------------------------------------

    def self.int_bounds?(min, max, low, high)
      min.is_a?(Integer) && max.is_a?(Integer) && low <= min && min <= max && max <= high
    end

    # [max_integer_value, bits, delta] for a compressed_float declaration.
    #
    # min, max and res round to float32 first, as the float parameter type of
    # the family does. nil for a declaration with no wire meaning: min not below
    # max, res not positive, or delta or delta / res not finite in float32.
    def self.compressed_float_params(min, max, res)
      return nil unless number?(min) && number?(max) && number?(res)

      lo = float32_round(min)
      hi = float32_round(max)
      r = float32_round(res)
      return nil unless lo < hi && r > 0.0

      delta = float32_round(hi - lo)
      values = float32_round(delta / r)
      return nil unless delta.finite? && values.finite?

      # clamped to [1, COMPRESSED_FLOAT_VALUES_MAX]
      values = 1.0 unless values >= 1.0
      values = COMPRESSED_FLOAT_VALUES_MAX if values > COMPRESSED_FLOAT_VALUES_MAX
      max_integer_value = values.ceil
      [max_integer_value, max_integer_value.bit_length, delta]
    end

    def self.precomputed_ok?(max_integer_value, bits, delta, min)
      return false unless max_integer_value.is_a?(Integer) && bits.is_a?(Integer)
      return false unless number?(delta) && number?(min)
      return false unless max_integer_value.between?(1, 4_294_967_040)
      return false unless bits == max_integer_value.bit_length

      d = float32_round(delta)
      d > 0.0 && d.finite? && float32_round(min).finite?
    end

    # [bits, raw_min, raw_span] for a fixed point declaration, or nil.
    #
    # The storage width integer_bits + fraction_bits is 8, 16, 32, 64 or 128,
    # the sign bit counting toward integer_bits; min and max are whole units
    # with min <= max, within int64 for storage of 64 and 128 bits, and within
    # the Q format's whole-unit capacity, read as signed exactly when min < 0.
    def self.fixed_params(integer_bits, fraction_bits, min, max)
      return nil unless integer_bits.is_a?(Integer) && fraction_bits.is_a?(Integer)
      return nil unless min.is_a?(Integer) && max.is_a?(Integer)
      return nil if integer_bits < 1 || fraction_bits.negative?

      width = integer_bits + fraction_bits
      return nil unless [8, 16, 32, 64, 128].include?(width)
      return nil if min > max
      return nil if width >= 64 && !(min >= INT64_MIN && max <= INT64_MAX)

      if min.negative?
        return nil if min < -(1 << (integer_bits - 1)) || max > (1 << (integer_bits - 1)) - 1
      elsif max > (1 << integer_bits) - 1
        return nil
      end
      raw_min = min << fraction_bits
      raw_span = (max - min) << fraction_bits
      [raw_span.bit_length, raw_min, raw_span]
    end

    # Bounds text for serialize_string with buffer_size = dest_size.
    #
    # IMPLEMENTING.md U15: the UTF-8 bytes before the first NUL; past
    # dest_size - 1 of them, the first dest_size - 1, then bytes dropped from
    # the end while the first byte left out is a continuation byte. Returns that
    # prefix as a UTF-8 String. dest_size below 1, and a source that is not
    # valid UTF-8 text, are write misuse.
    def self.copy_string(source, dest_size)
      unless dest_size.is_a?(Integer) && dest_size >= 1
        raise WriteMisuse, 'copy_string: dest_size must be an Integer of at least 1'
      end
      raise WriteMisuse, 'copy_string: source must be valid UTF-8 text' unless utf8_text?(source)

      data = source.b
      nul = data.index("\0")
      data = data.byteslice(0, nul) if nul
      limit = dest_size - 1
      if data.bytesize > limit
        n = limit
        n -= 1 while n.positive? && (data.getbyte(n) & 0xC0) == 0x80
        data = data.byteslice(0, n)
      end
      data.force_encoding(Encoding::UTF_8)
    end

    # ---------------------------------------------------------------------------
    # write and measure encodings: each validates the caller's value and
    # declaration and returns what goes on the wire, or raises WriteMisuse
    # ---------------------------------------------------------------------------

    # @api private
    module Encode
      module_function

      def bits(value, bits)
        unless bits.is_a?(Integer) && bits >= 1 && bits <= 64
          raise WriteMisuse, "serialize_bits: bits must be an Integer in [1, 64], got #{bits.inspect}"
        end
        unless value.is_a?(Integer) && value >= 0 && value < (1 << bits)
          raise WriteMisuse, "serialize_bits: #{value.inspect} does not fit #{bits} bits"
        end

        value
      end

      def bool(value)
        raise WriteMisuse, "serialize_bool: #{value.inspect} is not true or false" unless [true, false].include?(value)

        value ? 1 : 0
      end

      # [offset, bits] of a ranged field
      def ranged(value, min, max, low, high, what)
        unless Serialize.int_bounds?(min, max, low, high)
          raise WriteMisuse, "#{what}: min and max must be Integers with #{low} <= min <= max <= #{high}, " \
                             "got #{min.inspect}, #{max.inspect}"
        end
        unless value.is_a?(Integer) && value >= min && value <= max
          raise WriteMisuse, "#{what}: #{value.inspect} is outside [#{min}, #{max}]"
        end

        [value - min, (max - min).bit_length]
      end

      def uint128(value)
        unless value.is_a?(Integer) && value >= 0 && value < UINT128_LIMIT
          raise WriteMisuse, "serialize_uint128: #{value.inspect} does not fit 128 bits"
        end

        value
      end

      def fixed(value, integer_bits, fraction_bits, min, max)
        params = Serialize.fixed_params(integer_bits, fraction_bits, min, max)
        if params.nil?
          raise WriteMisuse, "serialize_fixed: (#{integer_bits.inspect}, #{fraction_bits.inspect}, " \
                             "#{min.inspect}, #{max.inspect}) is not a fixed point declaration"
        end
        n, raw_min, raw_span = params
        unless value.is_a?(Integer) && value >= raw_min && value <= raw_min + raw_span
          raise WriteMisuse, "serialize_fixed: raw value #{value.inspect} is outside [#{raw_min}, #{raw_min + raw_span}]"
        end

        [value - raw_min, n]
      end

      # [payload, bits] of an int_relative, flags included, LSB first
      def relative(previous, current)
        unless previous.is_a?(Integer) && current.is_a?(Integer)
          raise WriteMisuse, 'serialize_int_relative: previous and current must be Integers'
        end
        unless previous >= 0 && previous < current && current <= RELATIVE_MAX
          raise WriteMisuse, "serialize_int_relative: need 0 <= previous < current <= #{RELATIVE_MAX}, " \
                             "got #{previous}, #{current}"
        end

        difference = current - previous
        return [1, 1] if difference == 1

        TIERS.each do |(flags, lo, hi)|
          next unless difference.between?(lo, hi)

          # flags - 1 zero flags, then a one flag, then the payload
          return [(1 << (flags - 1)) | ((difference - lo) << flags), flags + (hi - lo).bit_length]
        end
        # six zero flags, then current as 32 raw bits
        [current << 6, 6 + 32]
      end

      def float(value)
        raise WriteMisuse, "serialize_float: #{value.inspect} is not a number" unless Serialize.number?(value)

        bits = Serialize.float32_bits(value)
        return bits if Serialize.float_bits_finite?(bits)

        raise WriteMisuse, "serialize_float: #{value.inspect} is not finite in float32"
      end

      def double(value)
        raise WriteMisuse, "serialize_double: #{value.inspect} is not a number" unless Serialize.number?(value)

        bits = Serialize.float64_bits(value)
        raise WriteMisuse, "serialize_double: #{value.inspect} is not finite" unless Serialize.double_bits_finite?(bits)

        bits
      end

      # [max_integer_value, bits, delta, min32] of a declaration
      def cf_declaration(min, max, res)
        params = Serialize.compressed_float_params(min, max, res)
        if params.nil?
          raise WriteMisuse, "serialize_compressed_float: (#{min.inspect}, #{max.inspect}, #{res.inspect}) " \
                             'is not a compressed float declaration'
        end
        params << Serialize.float32_round(min)
      end

      # [delta32, min32] of precomputed constants
      def cf_precomputed(max_integer_value, bits, delta, min)
        unless Serialize.precomputed_ok?(max_integer_value, bits, delta, min)
          raise WriteMisuse,
                "serialize_compressed_float_precomputed: the constants are not a compressed float declaration's"
        end

        [Serialize.float32_round(delta), Serialize.float32_round(min)]
      end

      # the quantized integer: float32 arithmetic, two roundings, the clamp
      def compressed(value, max_integer_value, delta, min32)
        unless Serialize.float32_round(min32 + delta).finite?
          raise WriteMisuse, "serialize_compressed_float: the declaration's top, min + delta, is not finite in float32"
        end
        raise WriteMisuse, "serialize_compressed_float: #{value.inspect} is not a number" unless Serialize.number?(value)

        value32 = Serialize.float32_round(value)
        raise WriteMisuse, "serialize_compressed_float: #{value.inspect} is not finite" unless value32.finite?

        normalized = Serialize.float32_round(Serialize.float32_round(value32 - min32) / delta)
        # clamped to [0, 1]; a NaN takes 0
        normalized = normalized >= 0.0 ? [normalized, 1.0].min : 0.0
        scaled = Serialize.float32_round(normalized * max_integer_value.to_f)
        [Serialize.float32_round(scaled + 0.5).floor, max_integer_value].min
      end

      def bytes(value, count)
        unless count.is_a?(Integer) && count >= 0
          raise WriteMisuse, "serialize_bytes: count must be a non-negative Integer, got #{count.inspect}"
        end
        raise WriteMisuse, 'serialize_bytes: the value must be a String' unless value.is_a?(String)
        unless value.bytesize == count
          raise WriteMisuse, "serialize_bytes: the value holds #{value.bytesize} bytes, the count is #{count}"
        end

        value
      end

      def buffer_size_ok?(buffer_size)
        buffer_size.is_a?(Integer) && buffer_size >= 1 && buffer_size <= INT32_MAX + 1
      end

      def string(value, buffer_size)
        unless buffer_size_ok?(buffer_size)
          raise WriteMisuse, "serialize_string: buffer_size must be an Integer in [1, 2^31], got #{buffer_size.inspect}"
        end
        raise WriteMisuse, 'serialize_string: the value must be valid UTF-8 text' unless Serialize.utf8_text?(value)
        raise WriteMisuse, 'serialize_string: the value holds a NUL' if value.include?("\0")
        if value.bytesize > buffer_size - 1
          raise WriteMisuse, "serialize_string: #{value.bytesize} bytes do not fit buffer_size #{buffer_size}"
        end

        value
      end
    end

    # ---------------------------------------------------------------------------
    # the streams
    # ---------------------------------------------------------------------------

    # The surface a serialize function is written against.
    #
    # WriteStream, ReadStream and MeasureStream implement every operation; a
    # serialize function takes a Stream and returns the boolean each call
    # returns, so it reads, writes and measures with one body.
    class Stream
      def writing?
        false
      end

      def reading?
        false
      end

      def serialize_uint8(ref)
        serialize_bits(ref, 8)
      end

      def serialize_uint16(ref)
        serialize_bits(ref, 16)
      end

      def serialize_uint32(ref)
        serialize_bits(ref, 32)
      end

      def serialize_uint64(ref)
        serialize_bits(ref, 64)
      end

      def bytes_processed
        (bits_processed + 7) >> 3
      end
    end

    # Writes bitpacked data into a buffer of capacity bytes.
    #
    # capacity is a multiple of 8 (IMPLEMENTING.md U1). An operation that does
    # not fit the room left raises WriteMisuse before any bit of it is written
    # (U9, U10). flush is required before data (U11).
    class WriteStream < Stream
      def initialize(capacity)
        unless capacity.is_a?(Integer) && capacity >= 0 && (capacity % 8).zero?
          raise WriteMisuse, "WriteStream: capacity must be a non-negative multiple of 8 bytes, got #{capacity.inspect}"
        end

        super()
        @capacity_bits = capacity * 8
        @out = String.new(capacity: capacity, encoding: Encoding::BINARY)
        @acc = 0
        @held = 0
        @bits = 0
      end

      def writing?
        true
      end

      def serialize_bits(ref, bits)
        value = Encode.bits(ref.value, bits)
        room(bits)
        put(value, bits)
        true
      end

      def serialize_bool(ref)
        value = Encode.bool(ref.value)
        room(1)
        put(value, 1)
        true
      end

      def serialize_int(ref, min, max)
        offset, n = Encode.ranged(ref.value, min, max, INT32_MIN, INT32_MAX, 'serialize_int')
        room(n)
        put(offset, n)
        true
      end

      def serialize_int64(ref, min, max)
        offset, n = Encode.ranged(ref.value, min, max, INT64_MIN, INT64_MAX, 'serialize_int64')
        room(n)
        put(offset, n)
        true
      end

      def serialize_int128(ref, min, max)
        offset, n = Encode.ranged(ref.value, min, max, INT128_MIN, INT128_MAX, 'serialize_int128')
        room(n)
        put(offset, n)
        true
      end

      def serialize_uint128(ref)
        value = Encode.uint128(ref.value)
        room(128)
        put(value, 128)
        true
      end

      def serialize_fixed(ref, integer_bits, fraction_bits, min, max)
        offset, n = Encode.fixed(ref.value, integer_bits, fraction_bits, min, max)
        room(n)
        put(offset, n)
        true
      end

      def serialize_int_relative(previous, ref)
        payload, n = Encode.relative(previous, ref.value)
        room(n)
        put(payload, n)
        true
      end

      def serialize_float(ref)
        bits = Encode.float(ref.value)
        room(32)
        put(bits, 32)
        true
      end

      def serialize_double(ref)
        bits = Encode.double(ref.value)
        room(64)
        put(bits, 64)
        true
      end

      def serialize_compressed_float(ref, min, max, res)
        max_integer_value, bits, delta, min32 = Encode.cf_declaration(min, max, res)
        integer = Encode.compressed(ref.value, max_integer_value, delta, min32)
        room(bits)
        put(integer, bits)
        true
      end

      def serialize_compressed_float_precomputed(ref, max_integer_value, bits, delta, min)
        delta32, min32 = Encode.cf_precomputed(max_integer_value, bits, delta, min)
        integer = Encode.compressed(ref.value, max_integer_value, delta32, min32)
        room(bits)
        put(integer, bits)
        true
      end

      def serialize_bytes(ref, count)
        data = Encode.bytes(ref.value, count)
        pad = (8 - (@bits & 7)) & 7
        room(pad + (8 * count))
        put(0, pad)
        put_aligned_bytes(data)
        true
      end

      def serialize_string(ref, buffer_size)
        data = Encode.string(ref.value, buffer_size)
        n = (buffer_size - 1).bit_length
        pad = (8 - ((@bits + n) & 7)) & 7
        room(n + pad + (8 * data.bytesize))
        put(data.bytesize, n + pad)
        put_aligned_bytes(data)
        true
      end

      def serialize_align
        pad = (8 - (@bits & 7)) & 7
        room(pad)
        put(0, pad)
        true
      end

      def serialize_end
        true
      end

      def serialize_object
        yield(self) == true
      end

      def reject
        raise WriteMisuse, "reject: the caller's refusal is read only; a WriteStream has nothing to refuse"
      end

      def fail(_reason = nil)
        false
      end

      # Completes the stream (U11). Every writer calls it before data. The held
      # bits reach data either way, so it moves nothing; it is the family's
      # completion point, and no write follows it.
      def flush; end

      # The stream: bytes_processed bytes, the final byte's unused bits zero.
      def data
        out = @out.dup
        acc = @acc
        tail = (@held + 7) >> 3
        while tail.positive?
          out.append_as_bytes(acc & 0xFF)
          acc >>= 8
          tail -= 1
        end
        out
      end

      def bits_processed
        @bits
      end

      def bits_available
        @capacity_bits - @bits
      end

      def align_bits
        (8 - (@bits & 7)) & 7
      end

      private

      def room(count)
        return if @bits + count <= @capacity_bits

        raise WriteMisuse, "WriteStream: #{count} bits do not fit the #{@capacity_bits - @bits} bits left in the buffer"
      end

      def put(value, count)
        acc = @acc | (value << @held)
        held = @held + count
        while held >= 64
          [acc & M64].pack('Q<', buffer: @out)
          acc >>= 64
          held -= 64
        end
        @acc = acc
        @held = held
        @bits += count
      end

      # the stream is aligned here, so the held bits are whole bytes
      def put_aligned_bytes(data)
        acc = @acc
        held = @held
        while held.positive?
          @out.append_as_bytes(acc & 0xFF)
          acc >>= 8
          held -= 8
        end
        @acc = 0
        @held = 0
        @out.append_as_bytes(data)
        @bits += 8 * data.bytesize
      end
    end

    # Reads bitpacked data from a String's bytes, never past their length.
    #
    # The buffer contract: none. The reader takes the stream's exact bytes and
    # never looks past them. A refusal is a value: the read returns false,
    # leaves its Ref unwritten, and latches the first reason, which refusal
    # reports. Every later read refuses, consumes nothing and writes nothing,
    # zero-bit reads included. A ReadStream is not re-initialized (U13): make a
    # new one for new bytes.
    class ReadStream < Stream
      def initialize(data)
        super()
        @err = nil
        if data.is_a?(String)
          @data = data.b.freeze
          @nbits = 8 * @data.bytesize
        else
          @data = ''.b.freeze
          @err = VALUE_OUT_OF_RANGE
          @nbits = -1
        end
        # the bytes as little-endian 32-bit words, each an immediate Integer;
        # the zero bytes that complete the last word are never read, because
        # every read checks its bits against @nbits first
        @words = (@data + ("\0" * ((4 - (@data.bytesize & 3)) & 3))).unpack('L<*').freeze
        @bit = 0
      end

      def reading?
        true
      end

      def serialize_bits(ref, bits)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless bits.is_a?(Integer) && bits >= 1 && bits <= 64
        return refuse(PAST_END) if @bit + bits > @nbits

        ref.value = take(bits)
        true
      end

      def serialize_bool(ref)
        return false if @err
        return refuse(PAST_END) if @bit + 1 > @nbits

        ref.value = take(1) == 1
        true
      end

      def serialize_int(ref, min, max)
        return false if @err

        value = read_int32(min, max)
        return false unless value

        ref.value = value
        true
      end

      def serialize_int64(ref, min, max)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless Serialize.int_bounds?(min, max, INT64_MIN, INT64_MAX)

        span = max - min
        n = span.bit_length
        if n.zero?
          ref.value = min
          return true
        end
        return refuse(PAST_END) if @bit + n > @nbits

        offset = take(n)
        return refuse(VALUE_OUT_OF_RANGE) if offset > span

        ref.value = min + offset
        true
      end

      def serialize_int128(ref, min, max)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless Serialize.int_bounds?(min, max, INT128_MIN, INT128_MAX)

        span = max - min
        n = span.bit_length
        if n.zero?
          ref.value = min
          return true
        end
        return refuse(PAST_END) if @bit + n > @nbits

        offset = take(n)
        return refuse(VALUE_OUT_OF_RANGE) if offset > span

        ref.value = min + offset
        true
      end

      def serialize_uint128(ref)
        return false if @err
        return refuse(PAST_END) if @bit + 128 > @nbits

        ref.value = take(128)
        true
      end

      def serialize_fixed(ref, integer_bits, fraction_bits, min, max)
        return false if @err

        params = Serialize.fixed_params(integer_bits, fraction_bits, min, max)
        return refuse(VALUE_OUT_OF_RANGE) if params.nil?

        n, raw_min, raw_span = params
        if n.zero?
          ref.value = raw_min
          return true
        end
        return refuse(PAST_END) if @bit + n > @nbits

        offset = take(n)
        return refuse(VALUE_OUT_OF_RANGE) if offset > raw_span

        ref.value = raw_min + offset
        true
      end

      def serialize_int_relative(previous, ref)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless previous.is_a?(Integer) && previous >= 0 && previous <= RELATIVE_MAX
        # the one-bit tier
        return refuse(PAST_END) if @bit + 1 > @nbits

        if take(1) == 1
          current = previous + 1
          return refuse(VALUE_OUT_OF_RANGE) if current > RELATIVE_MAX || current <= previous

          ref.value = current
          return true
        end
        # the bounded tiers, one flag each
        TIERS.each do |(_flags, lo, hi)|
          return refuse(PAST_END) if @bit + 1 > @nbits
          next if take(1).zero?

          n = (hi - lo).bit_length
          return refuse(PAST_END) if @bit + n > @nbits

          offset = take(n)
          return refuse(VALUE_OUT_OF_RANGE) if offset > hi - lo

          current = previous + lo + offset
          return refuse(VALUE_OUT_OF_RANGE) if current > RELATIVE_MAX || current <= previous

          ref.value = current
          return true
        end
        # the absolute tier: current as 32 raw bits
        return refuse(PAST_END) if @bit + 32 > @nbits

        v = take(32)
        return refuse(VALUE_OUT_OF_RANGE) if v > RELATIVE_MAX || v <= previous
        return refuse(NON_CANONICAL) if v - previous <= LAST_BOUNDED_MAX

        ref.value = v
        true
      end

      def serialize_float(ref)
        return false if @err
        return refuse(PAST_END) if @bit + 32 > @nbits

        bits = take(32)
        return refuse(NON_FINITE_FLOAT) if bits.allbits?(0x7F800000)

        ref.value = [bits].pack('L<').unpack1('e')
        true
      end

      def serialize_double(ref)
        return false if @err
        return refuse(PAST_END) if @bit + 64 > @nbits

        bits = take(64)
        return refuse(NON_FINITE_FLOAT) if bits.allbits?(0x7FF0000000000000)

        ref.value = [bits].pack('Q<').unpack1('E')
        true
      end

      def serialize_compressed_float(ref, min, max, res)
        return false if @err

        params = Serialize.compressed_float_params(min, max, res)
        return refuse(VALUE_OUT_OF_RANGE) if params.nil?

        max_integer_value, n, delta = params
        read_compressed(ref, max_integer_value, n, delta, Serialize.float32_round(min))
      end

      def serialize_compressed_float_precomputed(ref, max_integer_value, bits, delta, min)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless Serialize.precomputed_ok?(max_integer_value, bits, delta, min)

        read_compressed(ref, max_integer_value, bits, Serialize.float32_round(delta), Serialize.float32_round(min))
      end

      def serialize_bytes(ref, count)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless count.is_a?(Integer) && count >= 0
        return false unless check_align
        return refuse(PAST_END) if @bit + (8 * count) > @nbits

        b = @bit >> 3
        @bit += 8 * count
        ref.value = @data.byteslice(b, count)
        true
      end

      def serialize_string(ref, buffer_size)
        return false if @err
        return refuse(VALUE_OUT_OF_RANGE) unless Encode.buffer_size_ok?(buffer_size)

        length = read_int32(0, buffer_size - 1)
        return false unless length
        return false unless check_align
        return refuse(PAST_END) if @bit + (8 * length) > @nbits

        b = @bit >> 3
        @bit += 8 * length
        payload = @data.byteslice(b, length)
        return refuse(EMBEDDED_NUL) if payload.include?("\0")

        text = payload.force_encoding(Encoding::UTF_8)
        return refuse(INVALID_UTF8) unless text.valid_encoding?

        ref.value = text
        true
      end

      def serialize_align
        return false if @err

        check_align
      end

      def serialize_end
        return false if @err

        remaining = @nbits - @bit
        return refuse(TRAILING_DATA) if remaining >= 8
        return refuse(NONZERO_PADDING) if remaining > 0 && take(remaining) != 0

        true
      end

      def serialize_object
        return false if @err
        return true if yield(self) == true && @err.nil?

        refuse(REJECTED)
      end

      # The caller's refusal: latches rejected, keeping a first reason.
      def reject
        refuse(REJECTED)
      end

      # Refuses with a reason of the caller's; outside the list it is rejected.
      def fail(reason = nil)
        refuse(REASONS.include?(reason) ? reason : REJECTED)
      end

      # The first refusal's reason, or nil while the stream is healthy.
      def refusal
        @err
      end

      def bits_processed
        @bit
      end

      def bits_remaining
        remaining = @nbits - @bit
        remaining.negative? ? 0 : remaining
      end

      def align_bits
        (8 - (@bit & 7)) & 7
      end

      private

      # the latch (IMPLEMENTING.md, "Failure"): the first reason is kept, and
      # the readable length is poisoned, so the past-end check every read
      # already makes refuses every later read that would consume a bit
      def refuse(reason)
        @err ||= reason
        @nbits = -1
        false
      end

      # count bits at the read position, which the caller has checked are there
      def take(count)
        i = @bit
        @bit = i + count
        words = @words
        w = i >> 5
        s = i & 31
        v = words[w] >> s
        got = 32 - s
        while got < count
          w += 1
          v |= words[w] << got
          got += 32
        end
        v & ((1 << count) - 1)
      end

      def check_align
        pad = (8 - (@bit & 7)) & 7
        return refuse(NONZERO_PADDING) if pad != 0 && take(pad) != 0

        true
      end

      # a ranged int32 field, or false after a refusal; the latch is checked
      def read_int32(min, max)
        return refuse(VALUE_OUT_OF_RANGE) unless Serialize.int_bounds?(min, max, INT32_MIN, INT32_MAX)

        span = max - min
        n = span.bit_length
        return min if n.zero?
        return refuse(PAST_END) if @bit + n > @nbits

        offset = take(n)
        return refuse(VALUE_OUT_OF_RANGE) if offset > span

        min + offset
      end

      def read_compressed(ref, max_integer_value, count, delta, min32)
        return refuse(PAST_END) if @bit + count > @nbits

        integer = take(count)
        return refuse(VALUE_OUT_OF_RANGE) if integer > max_integer_value

        normalized = Serialize.float32_round(integer.to_f / max_integer_value)
        scaled = Serialize.float32_round(normalized * delta)
        decoded = Serialize.float32_round(scaled + min32)
        return refuse(NON_FINITE_FLOAT) unless decoded.finite?

        ref.value = decoded
        true
      end
    end

    # Counts the bits a message would take from any starting bit position.
    #
    # A bound, not the packet size: every alignment-performing operation (align,
    # bytes, string) costs 7 bits. It makes every check a write makes except the
    # room left, raises WriteMisuse the same way, and refuses nothing (U6).
    class MeasureStream < Stream
      def initialize
        super
        @bits = 0
      end

      def writing?
        true
      end

      def serialize_bits(ref, bits)
        Encode.bits(ref.value, bits)
        @bits += bits
        true
      end

      def serialize_bool(ref)
        Encode.bool(ref.value)
        @bits += 1
        true
      end

      def serialize_int(ref, min, max)
        @bits += Encode.ranged(ref.value, min, max, INT32_MIN, INT32_MAX, 'serialize_int')[1]
        true
      end

      def serialize_int64(ref, min, max)
        @bits += Encode.ranged(ref.value, min, max, INT64_MIN, INT64_MAX, 'serialize_int64')[1]
        true
      end

      def serialize_int128(ref, min, max)
        @bits += Encode.ranged(ref.value, min, max, INT128_MIN, INT128_MAX, 'serialize_int128')[1]
        true
      end

      def serialize_uint128(ref)
        Encode.uint128(ref.value)
        @bits += 128
        true
      end

      def serialize_fixed(ref, integer_bits, fraction_bits, min, max)
        @bits += Encode.fixed(ref.value, integer_bits, fraction_bits, min, max)[1]
        true
      end

      def serialize_int_relative(previous, ref)
        @bits += Encode.relative(previous, ref.value)[1]
        true
      end

      def serialize_float(ref)
        Encode.float(ref.value)
        @bits += 32
        true
      end

      def serialize_double(ref)
        Encode.double(ref.value)
        @bits += 64
        true
      end

      def serialize_compressed_float(ref, min, max, res)
        max_integer_value, bits, delta, min32 = Encode.cf_declaration(min, max, res)
        Encode.compressed(ref.value, max_integer_value, delta, min32)
        @bits += bits
        true
      end

      def serialize_compressed_float_precomputed(ref, max_integer_value, bits, delta, min)
        delta32, min32 = Encode.cf_precomputed(max_integer_value, bits, delta, min)
        Encode.compressed(ref.value, max_integer_value, delta32, min32)
        @bits += bits
        true
      end

      def serialize_bytes(ref, count)
        Encode.bytes(ref.value, count)
        @bits += 7 + (8 * count)
        true
      end

      def serialize_string(ref, buffer_size)
        data = Encode.string(ref.value, buffer_size)
        @bits += (buffer_size - 1).bit_length + 7 + (8 * data.bytesize)
        true
      end

      def serialize_align
        @bits += 7
        true
      end

      def serialize_end
        true
      end

      def serialize_object
        yield(self) == true
      end

      def reject
        raise WriteMisuse, "reject: the caller's refusal is read only; a MeasureStream has nothing to refuse"
      end

      def fail(_reason = nil)
        false
      end

      def bits_processed
        @bits
      end

      def align_bits
        7
      end
    end
  end

  Stream = Serialize::Stream
  WriteStream = Serialize::WriteStream
  ReadStream = Serialize::ReadStream
  MeasureStream = Serialize::MeasureStream
end
