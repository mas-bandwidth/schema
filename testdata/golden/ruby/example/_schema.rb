# frozen_string_literal: true

# The schema runtime of one generated unit (docs/PORT-RUBY.md, ERRORS). The
# compiler emits this file as <package>/_schema.rb with the module line naming
# the unit's own module, so every unit defines its own pair of errors and two
# units never share or shadow each other's.
module Example
  # A read refused its input. No value comes with it, and the caller's
  # objects are untouched. reason is the refusal's name, a diagnostic that is
  # never compared across languages.
  class Refused < StandardError
    attr_reader :reason

    def initialize(reason, detail = nil)
      @reason = reason.to_s
      super(detail.nil? ? @reason : "#{@reason}: #{detail}")
    end
  end

  # A write refused a value outside its contract, before writing a byte. path
  # names the field and rule the broken rule.
  class InvalidValue < StandardError
    attr_reader :path, :rule

    def initialize(path, rule)
      @path = path
      @rule = rule
      super("#{path}: #{rule}")
    end
  end

  # Optional table load report counters, written on success only.
  class LoadReport
    attr_accessor :unknown, :widened, :retained, :retain_lost

    def initialize
      @unknown = 0
      @widened = 0
      @retained = 0
      @retain_lost = 0
    end
  end

  def self._s_validate_int(value, path, min, max)
    raise InvalidValue.new(path, "expected Integer") unless value.is_a?(::Integer)
    return unless value < min || value > max
    raise InvalidValue.new(path, "#{value} is outside [#{min}, #{max}]")
  end

  def self._s_validate_bool(value, path)
    return if value.equal?(true) || value.equal?(false)
    raise InvalidValue.new(path, "expected true or false")
  end

  def self._s_validate_array(value, path, min, max)
    raise InvalidValue.new(path, "expected Array") unless value.instance_of?(::Array)
    n = value.length
    return unless n < min || n > max
    raise InvalidValue.new(path, "#{n} elements, expected #{max}") if min == max
    raise InvalidValue.new(path, "#{n} elements is outside [#{min}, #{max}]")
  end

  def self._s_validate_string(value, path, size)
    raise InvalidValue.new(path, "expected String") unless value.is_a?(::String)
    enc = value.encoding
    unless enc == ::Encoding::UTF_8 || enc == ::Encoding::US_ASCII
      raise InvalidValue.new(path, "expected UTF-8 or US-ASCII String, got #{enc.name}")
    end
    raise InvalidValue.new(path, "invalid UTF-8") unless value.valid_encoding?
    raise InvalidValue.new(path, "holds a NUL") if value.include?("\x00")
    return unless value.bytesize > size
    raise InvalidValue.new(path, "#{value.bytesize} bytes of UTF-8 exceed the bound #{size}")
  end

  def self._s_validate_bytes(value, path, size)
    unless value.is_a?(::String) && value.encoding == ::Encoding::BINARY
      raise InvalidValue.new(path, "expected ASCII-8BIT String")
    end
    return unless value.bytesize > size
    raise InvalidValue.new(path, "#{value.bytesize} bytes exceed the bound #{size}")
  end

  def self._s_finite_float32(value, path)
    raise InvalidValue.new(path, "expected Float") unless value.is_a?(::Float)
    raise InvalidValue.new(path, "#{value} is not finite") unless value.finite?
    rounded = Serialize.float32_round(value)
    raise InvalidValue.new(path, "#{value} rounds to an infinity in float32") unless rounded.finite?
    rounded
  end

  def self._s_validate_float32(value, path)
    _s_finite_float32(value, path)
    nil
  end

  def self._s_validate_float64(value, path)
    raise InvalidValue.new(path, "expected Float") unless value.is_a?(::Float)
    raise InvalidValue.new(path, "#{value} is not finite") unless value.finite?
  end

  def self._s_validate_compressed_float(value, path, min, max)
    rounded = _s_finite_float32(value, path)
    return unless rounded < min || rounded > max
    raise InvalidValue.new(path, "#{value} is outside [#{min}, #{max}]")
  end

  def self._s_float32_bits_eq(a, b)
    return a == b unless a.is_a?(::Float) && b.is_a?(::Float)
    [Serialize.float32_round(a)].pack("e") == [Serialize.float32_round(b)].pack("e")
  end

  def self._s_float64_bits_eq(a, b)
    return a == b unless a.is_a?(::Float) && b.is_a?(::Float)
    [a].pack("E") == [b].pack("E")
  end

  def self._s_copy_bytes(data)
    out = ::String.new(capacity: data.bytesize, encoding: ::Encoding::BINARY)
    out << data
    out
  end

  def self._s_open_read_stream(data, bit_offset)
    raise Refused.new("bad_input", "expected String") unless data.is_a?(::String)
    total_bits = data.bytesize * 8
    unless bit_offset.is_a?(::Integer) && bit_offset >= 0 && bit_offset <= total_bits
      raise Refused.new("bad_bit_offset", "bit_offset outside [0, #{total_bits}]")
    end
    byte_off = bit_offset >> 3
    rem = bit_offset & 7
    slice = byte_off == 0 ? data : (data.byteslice(byte_off, data.bytesize - byte_off) || "".b)
    stream = ReadStream.new(slice)
    if rem > 0
      skip = Serialize::Ref.new(0)
      raise Refused.new(stream.refusal || "past_end") unless stream.serialize_bits(skip, rem)
    end
    [stream, rem]
  end
end
