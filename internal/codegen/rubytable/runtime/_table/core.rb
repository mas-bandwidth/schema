# frozen_string_literal: true

module SchemaRuntimeUnit
  # Explicit shared byte or string blob node for form-1 and cooked writes.
  class BlobRef
    attr_reader :value

    def initialize(value)
      @value = value
    end

    def ==(other)
      other.is_a?(BlobRef) && @value.encoding == other.value.encoding && @value.bytes == other.value.bytes
    end
  end

  # Internal table runtime scoped to this unit module.
  module SchemaTable
    def self.u(val)
      val
    end

    def self.arr
      # @type var out: ::Array[untyped]
      out = []
      out.itself
    end

    def self.map_h
      # @type var out: ::Hash[untyped, untyped]
      out = {}
      out.itself
    end

    EMPTY_ARR = arr.freeze
    EMPTY_HASH = map_h.freeze
    FLOAT32_MAX = 3.4028234663852886e+38
    FLOAT32_OVERFLOW = 3.4028235677973366e+38
    COMPRESSED_FLOAT_VALUES_MAX = 4_294_967_040.0
    TEXT_ENCODINGS = [Encoding::UTF_8, Encoding::US_ASCII].freeze

    CRC32C_TABLE = (0..255).map do |n|
      crc = n
      8.times { crc = (crc >> 1) ^ (crc.allbits?(1) ? 0x82F63B78 : 0) }
      crc
    end.freeze

    def self.crc32c(data)
      val = 0xFFFFFFFF
      data.each_byte do |byte|
        val = CRC32C_TABLE[(val ^ byte) & 0xFF] ^ (val >> 8)
      end
      val ^ 0xFFFFFFFF
    end

    def self.seal_crc(data)
      b = data.b
      b + [crc32c(b)].pack('V')
    end

    def self.open_crc(data, form = 1)
      raw = binary_input(data)
      if raw.empty?
        raise Refused.new(form == 3 ? 'truncated' : 'wire_damaged', 'empty file')
      end

      first = raw.getbyte(0)
      if first != form
        reason = if form == 1
                   case first
                   when 2 then 'message_form_as_file'
                   when 3 then 'fixed_form_as_file'
                   else 'newer_form'
                   end
                 elsif form == 3
                   case first
                   when 1 then 'previous_form'
                   when 2 then 'message_form_as_file'
                   else 'newer_form'
                   end
                 else
                   'newer_form'
                 end
        raise Refused.new(reason, 'wrong form')
      end

      min_len = form == 2 ? 6 : 5
      raise Refused.new('integrity_mismatch', 'file is shorter than its checksum') if raw.bytesize < min_len

      body = raw.byteslice(0, raw.bytesize - 4)
      expected = raw.byteslice(raw.bytesize - 4, 4).unpack1('V')
      raise Refused.new('integrity_mismatch', 'CRC-32C does not match') if expected != crc32c(body)

      body
    end

    def self.binary_input(data)
      raise Refused.new('invalid_buffer', 'a read takes a binary String') unless data.is_a?(String)

      data.encoding == Encoding::ASCII_8BIT ? data : data.b
    end

    def self.float32_round(value)
      narrowed = u([value].pack('e').unpack1('e'))
      return narrowed unless narrowed.infinite? && value.abs < FLOAT32_OVERFLOW

      value.negative? ? -FLOAT32_MAX : FLOAT32_MAX
    end

    # A compressed float's declared range, compared at float32 (SPEC.md 4.6):
    # the descriptor carries the float32 ends, and the value is rounded to
    # float32 before it is compared.
    def self.float_in_range?(field, value)
      float32_round(value).between?(field.fmin, field.fmax)
    end

    def self.finite_float32(value, path)
      raise InvalidValue.new(path, 'expected Float') unless value.instance_of?(Float)
      raise InvalidValue.new(path, "#{value.inspect} is not finite") unless value.finite?

      rounded = float32_round(value)
      raise InvalidValue.new(path, "#{value.inspect} overflows float32") unless rounded.finite?

      rounded
    end

    def self.float32_bits(value)
      [float32_round(value)].pack('e').unpack1('V')
    end

    def self.float32_from_bits(bits)
      [bits].pack('V').unpack1('e')
    end

    def self.float64_bits(value)
      [value].pack('E').unpack1('Q<')
    end

    def self.float64_from_bits(bits)
      [bits].pack('Q<').unpack1('E')
    end

    def self.float32_bits_eq?(lhs, rhs)
      return lhs == rhs unless lhs.instance_of?(Float) && rhs.instance_of?(Float)

      r_lhs = float32_round(lhs)
      r_rhs = float32_round(rhs)
      return float64_bits(lhs) == float64_bits(rhs) unless r_lhs.finite? && r_rhs.finite?

      float32_bits(r_lhs) == float32_bits(r_rhs)
    end

    def self.float64_bits_eq?(lhs, rhs)
      return lhs == rhs unless lhs.instance_of?(Float) && rhs.instance_of?(Float)

      float64_bits(lhs) == float64_bits(rhs)
    end

    def self.validate_bool(value, path)
      return if value.equal?(true) || value.equal?(false)

      raise InvalidValue.new(path, 'expected true or false')
    end

    def self.validate_int(value, path, min_v, max_v)
      raise InvalidValue.new(path, 'expected Integer') unless value.instance_of?(Integer)
      return if value.between?(min_v, max_v)

      raise InvalidValue.new(path, "#{value} is outside [#{min_v}, #{max_v}]")
    end

    def self.validate_float32(value, path)
      finite_float32(value, path)
      nil
    end

    def self.validate_float64(value, path)
      raise InvalidValue.new(path, 'expected Float') unless value.instance_of?(Float)
      raise InvalidValue.new(path, "#{value.inspect} is not finite") unless value.finite?
    end

    def self.validate_compressed_float(value, path, min_v, max_v)
      rounded = finite_float32(value, path)
      return if rounded.between?(min_v, max_v)

      raise InvalidValue.new(path, "#{value.inspect} is outside [#{min_v.inspect}, #{max_v.inspect}]")
    end

    def self.validate_text(value, path, size)
      raise InvalidValue.new(path, 'expected String') unless value.instance_of?(String)
      raise InvalidValue.new(path, 'expected UTF-8 or US-ASCII String') unless TEXT_ENCODINGS.include?(value.encoding)
      raise InvalidValue.new(path, 'invalid UTF-8') unless value.valid_encoding?
      raise InvalidValue.new(path, 'holds a NUL byte') if value.include?("\x00")
      if size.positive? && value.bytesize > size
        raise InvalidValue.new(path, "#{value.bytesize} bytes exceed bound #{size}")
      end

      value.encoding == Encoding::UTF_8 ? value : value.encode(Encoding::UTF_8)
    end

    def self.validate_bytes(value, path, size)
      raise InvalidValue.new(path, 'expected ASCII-8BIT String') unless value.instance_of?(String)
      raise InvalidValue.new(path, 'expected ASCII-8BIT encoding') unless value.encoding == Encoding::ASCII_8BIT
      if size.positive? && value.bytesize > size
        raise InvalidValue.new(path, "#{value.bytesize} bytes exceed bound #{size}")
      end

      value
    end

    def self.pack_int_le(value, width, signed)
      out = String.new(capacity: width, encoding: Encoding::ASCII_8BIT)
      rem = signed && value.negative? ? value + (1 << (width * 8)) : value
      width.times do
        out << (rem & 0xFF)
        rem >>= 8
      end
      out
    end

    def self.unpack_int_le(raw, pos, width, signed)
      val = 0
      idx = width - 1
      while idx >= 0
        val = (val << 8) | raw.getbyte(pos + idx)
        idx -= 1
      end
      val -= (1 << (width * 8)) if signed && val >= (1 << ((width * 8) - 1))
      val
    end

    def self.pack_int_order(value, width, big_endian, signed)
      le = pack_int_le(value, width, signed)
      big_endian ? le.reverse : le
    end

    def self.unpack_int_order(raw, pos, width, big_endian, signed)
      return unpack_int_le(raw, pos, width, signed) unless big_endian

      val = 0
      width.times do |i|
        val = (val << 8) | raw.getbyte(pos + i)
      end
      val -= (1 << (width * 8)) if signed && val >= (1 << ((width * 8) - 1))
      val
    end
  end
end
