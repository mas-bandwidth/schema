# frozen_string_literal: true

# A measurement of the representation choices DESIGN.md gives speed as the
# reason for, not a test: each line packs or unpacks the same N fields through
# one candidate representation, best of three runs, in nanoseconds per field.
# The candidates are reduced to the inner loop the library runs (no checks,
# no Ref), so the numbers compare representations, not the library's calls;
# `make bench` measures the library itself. Reads the monotonic clock, so it is
# run only by make bench-choices.
#
#   usage: <ruby> [--yjit] test/bench_choices.rb [N]

N = Integer(ARGV.fetch(0, '200000'))
M64 = (1 << 64) - 1
M32 = (1 << 32) - 1

# The writer's candidates: each takes [value, bits] pairs and returns the bytes.
module Writers
  module_function

  # lib's: an Integer accumulator, each whole 64-bit word appended to a binary String
  def word64(fields)
    out = String.new(encoding: Encoding::BINARY)
    acc = 0
    held = 0
    fields.each do |value, bits|
      acc |= value << held
      held += bits
      while held >= 64
        [acc & M64].pack('Q<', buffer: out)
        acc >>= 64
        held -= 64
      end
    end
    out
  end

  # the same, emptied every 32 bits, so the accumulator stays below 2^64
  def word32(fields)
    out = String.new(encoding: Encoding::BINARY)
    acc = 0
    held = 0
    fields.each do |value, bits|
      acc |= value << held
      held += bits
      while held >= 32
        [acc & M32].pack('L<', buffer: out)
        acc >>= 32
        held -= 32
      end
    end
    out
  end

  # one byte at a time
  def byte8(fields)
    out = String.new(encoding: Encoding::BINARY)
    acc = 0
    held = 0
    fields.each do |value, bits|
      acc |= value << held
      held += bits
      while held >= 8
        out << (acc & 0xFF)
        acc >>= 8
        held -= 8
      end
    end
    out
  end
end

# The reader's candidates: each reads the widths in order and returns the sum.
module Readers
  module_function

  # lib's: the bytes as an Array of little-endian 32-bit words, each an immediate Integer
  def words32(data, widths)
    words = data.unpack('L<*')
    bit = 0
    sum = 0
    widths.each do |count|
      w = bit >> 5
      s = bit & 31
      v = words[w] >> s
      got = 32 - s
      while got < count
        w += 1
        v |= words[w] << got
        got += 32
      end
      sum += v & ((1 << count) - 1)
      bit += count
    end
    sum
  end

  # the same over 64-bit words, which are not all immediates
  def words64(data, widths)
    words = data.unpack('Q<*')
    bit = 0
    sum = 0
    widths.each do |count|
      w = bit >> 6
      s = bit & 63
      v = words[w] >> s
      got = 64 - s
      while got < count
        w += 1
        v |= words[w] << got
        got += 64
      end
      sum += v & ((1 << count) - 1)
      bit += count
    end
    sum
  end

  # an unpack of the String at each read's byte offset
  def unpack_at(data, widths)
    bit = 0
    sum = 0
    widths.each do |count|
      v = data.unpack1('Q<', offset: bit >> 3) >> (bit & 7)
      v |= data.getbyte((bit >> 3) + 8) << (64 - (bit & 7)) if (bit & 7) + count > 64
      sum += v & ((1 << count) - 1)
      bit += count
    end
    sum
  end
end

def clock
  Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond)
end

def best
  Array.new(3) do
    t = clock
    yield
    clock - t
  end.min
end

yjit = defined?(RubyVM::YJIT) && RubyVM::YJIT.enabled? ? 'on' : 'off'
puts "serialize.rb bench-choices: Ruby #{RUBY_VERSION} #{RUBY_PLATFORM}, YJIT #{yjit}, N = #{N}"
[['bits 5', 21, 5], ['bits 32', 0xDEADBEEF, 32]].each do |label, value, bits|
  fields = Array.new(N) { [value, bits] }
  widths = Array.new(N, bits)
  data = Writers.word64(fields)
  # every candidate makes the same bytes and reads the same fields back
  %i[word32 byte8].each { |name| raise "#{name} differs" unless Writers.public_send(name, fields) == data }
  data += "\0" * 16
  %i[words64 unpack_at].each do |name|
    raise "#{name} differs" unless Readers.public_send(name, data, widths) == Readers.words32(data, widths)
  end
  %i[word64 word32 byte8].each do |name|
    ns = best { Writers.public_send(name, fields) }
    puts format('write %<label>-8s %<name>-10s %<ns>8.1f ns', label: label, name: name, ns: ns.fdiv(N))
  end
  %i[words32 words64 unpack_at].each do |name|
    ns = best { Readers.public_send(name, data, widths) }
    puts format('read  %<label>-8s %<name>-10s %<ns>8.1f ns', label: label, name: name, ns: ns.fdiv(N))
  end
end
