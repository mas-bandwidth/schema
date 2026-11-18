# frozen_string_literal: true

# A measurement of the hot operations, not a test: each line is one operation
# written N times into one WriteStream, then read N times from one ReadStream,
# best of three runs, in nanoseconds per call. Reads the monotonic clock, so it
# is run only by make bench.
#
#   usage: <ruby> [--yjit] test/bench.rb [N]

$LOAD_PATH.unshift(File.join(File.dirname(__dir__), 'lib'))
require 'serialize'

N = Integer(ARGV.fetch(0, '200000'))

CASES = [
  ['bits 5', 21, ->(s, r) { s.serialize_bits(r, 5) }],
  ['bits 32', 0xDEADBEEF, ->(s, r) { s.serialize_bits(r, 32) }],
  ['int [-1000, 1000]', -321, ->(s, r) { s.serialize_int(r, -1000, 1000) }],
  ['int64 full', -(1 << 62), ->(s, r) { s.serialize_int64(r, -(1 << 63), (1 << 63) - 1) }],
  ['float', 1.5, ->(s, r) { s.serialize_float(r) }],
  ['double', 1.0 / 3.0, ->(s, r) { s.serialize_double(r) }],
  ['compressed_float', 5.25, ->(s, r) { s.serialize_compressed_float(r, 0.0, 10.0, 0.01) }],
  ['bytes 16', "\xAB".b * 16, ->(s, r) { s.serialize_bytes(r, 16) }]
].freeze

def clock
  Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond)
end

def best(runs)
  Array.new(3) do
    t = clock
    runs.call
    clock - t
  end.min
end

yjit = defined?(RubyVM::YJIT) && RubyVM::YJIT.enabled? ? 'on' : 'off'
puts "serialize.rb bench: Ruby #{RUBY_VERSION} #{RUBY_PLATFORM}, YJIT #{yjit}, N = #{N}"
puts "#{'operation'.ljust(20)} #{'write ns'.rjust(10)} #{'read ns'.rjust(10)}"
CASES.each do |name, value, op|
  ref = Serialize::Ref.new(value)
  data = nil
  write = best(lambda do
    ws = Serialize::WriteStream.new(((N * 160) + 63) / 64 * 8)
    i = 0
    while i < N
      op.call(ws, ref)
      i += 1
    end
    ws.flush
    data = ws.data
  end)
  out = Serialize::Ref.new
  read = best(lambda do
    rs = Serialize::ReadStream.new(data)
    i = 0
    while i < N
      raise "read refused: #{rs.refusal}" unless op.call(rs, out)

      i += 1
    end
  end)
  puts format('%<name>-20s %<write>10.1f %<read>10.1f', name: name, write: write.fdiv(N), read: read.fdiv(N))
end
