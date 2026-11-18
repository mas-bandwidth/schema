# frozen_string_literal: true

# The Ruby packet finite-float gate (SPEC §4.3, serialize format 2.0).
require File.expand_path("finite", ARGV.fetch(0))

$failures = 0
$vectors = 0

def fail_vec(name, detail)
  $stderr.write("FAILED: #{name}: #{detail}\n")
  $failures += 1
end

def f32(bits)
  [bits].pack("V").unpack1("e")
end

def f64(bits)
  [bits].pack("Q<").unpack1("E")
end

def bits32(val)
  [val].pack("e").unpack1("V")
end

def bits64(val)
  [val].pack("E").unpack1("Q<")
end

NON_FINITE_32 = [
  ["quiet-nan", 0x7FC00000], ["signalling-nan", 0x7F800001],
  ["negative-nan", 0xFFC00000], ["payload-nan", 0x7FC0DEAD],
  ["positive-inf", 0x7F800000], ["negative-inf", 0xFF800000]
].freeze

NON_FINITE_64 = [
  ["quiet-nan", 0x7FF8000000000000], ["signalling-nan", 0x7FF0000000000001],
  ["negative-nan", 0xFFF8000000000000], ["payload-nan", 0x7FF800000000DEAD],
  ["positive-inf", 0x7FF0000000000000], ["negative-inf", 0xFFF0000000000000]
].freeze

FINITE_32 = [
  ["negative-zero", 0x80000000], ["min-subnormal", 0x00000001],
  ["max-subnormal", 0x007FFFFF], ["negative-subnormal", 0x80000001],
  ["max-finite", 0x7F7FFFFF], ["negative-max-finite", 0xFF7FFFFF]
].freeze

FINITE_64 = [
  ["negative-zero", 0x8000000000000000], ["min-subnormal", 0x0000000000000001],
  ["max-subnormal", 0x000FFFFFFFFFFFFF], ["negative-subnormal", 0x8000000000000001],
  ["max-finite", 0x7FEFFFFFFFFFFFFF], ["negative-max-finite", 0xFFEFFFFFFFFFFFFF]
].freeze

QUANTIZED_BITS = 10

def build_finite
  v = Finite::Finite.new
  v.aligned = 1.5
  v.single = 2.5
  v.flag = true
  v.unaligned = 3.5
  v.quantized = 5.0
  v
end

def raw_base
  {
    "aligned" => bits64(1.5),
    "single" => bits32(2.5),
    "unaligned" => bits64(3.5),
    "quantized" => 500
  }
end

def raw_finite(r)
  s = Finite::WriteStream.new(64)
  s.serialize_bits(Finite::Serialize::Ref.new(r["aligned"]), 64)
  s.serialize_bits(Finite::Serialize::Ref.new(r["single"]), 32)
  s.serialize_bool(Finite::Serialize::Ref.new(true))
  s.serialize_bits(Finite::Serialize::Ref.new(r["unaligned"]), 64)
  s.serialize_bits(Finite::Serialize::Ref.new(r["quantized"]), QUANTIZED_BITS)
  s.flush
  s.data
end

def raw_pair(a, b)
  s = Finite::WriteStream.new(16)
  s.serialize_bits(Finite::Serialize::Ref.new(a), 64)
  s.serialize_bits(Finite::Serialize::Ref.new(b), 64)
  s.flush
  s.data
end

def check_write_refused(name, value, path)
  wire = value.write
  fail_vec(name, "write accepted a non-finite float (#{wire.unpack1("H*")})")
rescue Finite::InvalidValue => e
  fail_vec(name, "write refused at #{e.path}, want #{path}") if e.path != path
rescue StandardError => e
  fail_vec(name, "write raised #{e.class}: #{e}")
end

def check_read_refused(name, klass, wire)
  klass.read(wire)
  fail_vec(name, "read accepted a non-finite pattern")
rescue Finite::Refused => e
  fail_vec(name, "read refused with #{e.reason}, want non_finite_float") if e.reason != "non_finite_float"
rescue StandardError => e
  fail_vec(name, "the reader raised #{e.class}: #{e}")
end

def check_round_trip(name, value, klass, want)
  wire = value.write
  if wire != want
    fail_vec(name, "wire #{wire.unpack1("H*")}, want #{want.unpack1("H*")}: the pattern did not ride bit exact")
    return
  end
  got = klass.read(wire)
  fail_vec(name, "read back a value that is not the pattern written") unless yield(got)
rescue Finite::InvalidValue => e
  fail_vec(name, "write refused a finite value: #{e}")
rescue Finite::Refused => e
  fail_vec(name, "read refused a finite pattern: #{e.reason}")
end

POSITIONS_32 = ["single"].freeze
POSITIONS_64 = ["aligned", "unaligned"].freeze

NON_FINITE_32.each do |label, pattern|
  POSITIONS_32.each do |p|
    v = build_finite
    v.public_send(:"#{p}=", f32(pattern))
    check_write_refused("#{p}/write/#{label}", v, "Finite.#{p}")
    r = raw_base
    r[p] = pattern
    check_read_refused("#{p}/read/#{label}", Finite::Finite, raw_finite(r))
    $vectors += 2
  end
  q = build_finite
  q.quantized = f32(pattern)
  check_write_refused("quantized/write/#{label}", q, "Finite.quantized")
  $vectors += 1
end

NON_FINITE_64.each do |label, pattern|
  POSITIONS_64.each do |p|
    v = build_finite
    v.public_send(:"#{p}=", f64(pattern))
    check_write_refused("#{p}/write/#{label}", v, "Finite.#{p}")
    r = raw_base
    r[p] = pattern
    check_read_refused("#{p}/read/#{label}", Finite::Finite, raw_finite(r))
    $vectors += 2
  end
  pair = Finite::Pair.new
  pair.first = 1.0
  pair.second = f64(pattern)
  check_write_refused("pair/write/#{label}", pair, "Pair.second")
  check_read_refused("pair/read/#{label}", Finite::Pair, raw_pair(bits64(1.0), pattern))
  $vectors += 2
  q = build_finite
  q.quantized = f64(pattern)
  check_write_refused("quantized/write/double-#{label}", q, "Finite.quantized")
  $vectors += 1
end

v = build_finite
v.single = 1e39
check_write_refused("single/write/narrows-to-inf", v, "Finite.single")
$vectors += 1

v = build_finite
v.single = f32(0x7F7FFFFF) * (1.0 + (2.0**-26))
r = raw_base
r["single"] = 0x7F7FFFFF
check_round_trip("single/roundtrip/rounds-to-max-finite", v, Finite::Finite, raw_finite(r)) do |got|
  bits32(got.single) == 0x7F7FFFFF
end
$vectors += 1

FINITE_32.each do |label, pattern|
  POSITIONS_32.each do |p|
    v = build_finite
    v.public_send(:"#{p}=", f32(pattern))
    r = raw_base
    r[p] = pattern
    check_round_trip("#{p}/roundtrip/#{label}", v, Finite::Finite, raw_finite(r)) do |got|
      bits32(got.public_send(p.to_sym)) == pattern
    end
    $vectors += 1
  end
end

FINITE_64.each do |label, pattern|
  POSITIONS_64.each do |p|
    v = build_finite
    v.public_send(:"#{p}=", f64(pattern))
    r = raw_base
    r[p] = pattern
    check_round_trip("#{p}/roundtrip/#{label}", v, Finite::Finite, raw_finite(r)) do |got|
      bits64(got.public_send(p.to_sym)) == pattern
    end
    $vectors += 1
  end
  pair = Finite::Pair.new
  pair.first = f64(pattern)
  pair.second = 1.0
  check_round_trip("pair/roundtrip/#{label}", pair, Finite::Pair, raw_pair(pattern, bits64(1.0))) do |got|
    bits64(got.first) == pattern
  end
  $vectors += 1
end

if $failures > 0
  $stderr.write("packet finite Ruby: #{$failures} of #{$vectors} vectors FAILED\n")
  exit 1
end
puts "OK: packet finite Ruby, #{$vectors} vectors"
