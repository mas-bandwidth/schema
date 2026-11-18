# frozen_string_literal: true

# The Ruby half of test/packet-void (SPEC §4.8).
require File.expand_path("packetvoidruby", ARGV.fetch(0))

def fail_void(msg)
  $stderr.write("FAILED: #{msg}\n")
  exit 1
end

def read_goldens(path)
  out = {}
  File.readlines(path).each do |line|
    line = line.sub(/#.*/, "").strip
    next if line.empty?
    name, hex = line.split
    out[name] = (hex == "-" || hex == "--") ? "".b : [hex].pack("H*")
  end
  out
end

def build_case(name)
  case name
  when "mixed-none"
    [Packetvoidruby::Mixed.new, Packetvoidruby::Mixed, 2]
  when "mixed-ping"
    v = Packetvoidruby::Mixed.new
    v.type = Packetvoidruby::MixedType::Ping
    [v, Packetvoidruby::Mixed, 2]
  when "mixed-pong"
    v = Packetvoidruby::Mixed.new
    v.type = Packetvoidruby::MixedType::Pong
    [v, Packetvoidruby::Mixed, 2]
  when "mixed-payload"
    v = Packetvoidruby::Mixed.new
    v.type = Packetvoidruby::MixedType::Payload
    [v, Packetvoidruby::Mixed, 19]
  when "frame-ping"
    v = Packetvoidruby::Frame.new
    v.lead = true
    v.choice.type = Packetvoidruby::MixedType::Ping
    v.tail = true
    [v, Packetvoidruby::Frame, 4]
  when "tags-none"
    [Packetvoidruby::Tags.new, Packetvoidruby::Tags, 2]
  when "tags-pong"
    v = Packetvoidruby::Tags.new
    v.type = Packetvoidruby::TagsType::Pong
    [v, Packetvoidruby::Tags, 2]
  when "single-ack"
    v = Packetvoidruby::Single.new
    v.type = Packetvoidruby::SingleType::Ack
    [v, Packetvoidruby::Single, 1]
  when "empty-none"
    [Packetvoidruby::Empty.new, Packetvoidruby::Empty, 0]
  else
    fail_void("unknown golden #{name}")
  end
end

def expect_invalid(label, value)
  value.write
  fail_void("write #{label} was accepted")
rescue Packetvoidruby::InvalidValue
  nil
end

def expect_refused(label, klass, data)
  klass.read(data)
  fail_void("read #{label} was accepted")
rescue Packetvoidruby::Refused
  nil
end

if Packetvoidruby::MixedType::None != 0 || Packetvoidruby::MixedType::Ping != 1 ||
   Packetvoidruby::MixedType::Payload != 2 || Packetvoidruby::MixedType::Pong != 3 ||
   Packetvoidruby::MixedType::Max != 3
  fail_void("MixedType tag numbers")
end
if Packetvoidruby::MixedType::NAMES != { 0 => "None", 1 => "ping", 2 => "payload", 3 => "pong" } ||
   !Packetvoidruby::MixedType::NAMES.frozen?
  fail_void("MixedType name table")
end
if Packetvoidruby::TagsType::Max != 2 || Packetvoidruby::TagsType::Pong != 2 ||
   Packetvoidruby::SingleType::Max != 1 || Packetvoidruby::SingleType::Ack != 1 ||
   Packetvoidruby::EmptyType::Max != 0
  fail_void("payload-free tag extents")
end

fresh = Packetvoidruby::Mixed.new
fail_void("union construction") unless fresh.type == Packetvoidruby::MixedType::None && fresh.payload.instance_of?(Packetvoidruby::Payload)
fail_void("payload arm declared defaults") unless fresh.payload.count == -1 && fresh.payload.marker.equal?(true)
fail_void("union field construction") unless Packetvoidruby::Frame.new.choice.type == Packetvoidruby::MixedType::None

goldens_path = File.join(__dir__, "goldens.txt")
read_goldens(goldens_path).each do |name, wire|
  value, klass, want_bits = build_case(name)
  got = value.write
  fail_void("write #{name}: got #{got.unpack1("H*")} want #{wire.unpack1("H*")}") if got != wire
  back, bits = klass.read_bits(wire)
  fail_void("read_bits #{name}: got #{bits} bits, want #{want_bits}") if bits != want_bits
  fail_void("read #{name} value differs: #{back.inspect}") if back != value
  fail_void("read #{name} write-back differs") if back.write != wire
  if name.start_with?("mixed-")
    payload = back.payload
    fail_void("mixed arm defaults after #{name}") unless payload.instance_of?(Packetvoidruby::Payload) && payload.count == -1 && payload.marker.equal?(true)
  end
end

selected = Packetvoidruby::Mixed.read("\x01".b)
fail_void("payload defaults after void selection") unless selected.type == Packetvoidruby::MixedType::Ping && selected.payload.count == -1 && selected.payload.marker.equal?(true)

bad = Packetvoidruby::Mixed.new
bad.type = 4
expect_invalid("mixed-tag-4", bad)
bad = Packetvoidruby::Mixed.new
bad.type = -1
expect_invalid("mixed-tag-negative", bad)
bad = Packetvoidruby::Mixed.new
bad.type = true
expect_invalid("mixed-tag-bool", bad)
bad = Packetvoidruby::Mixed.new
bad.type = Packetvoidruby::MixedType::Payload
bad.payload = 1
expect_invalid("mixed-payload-type", bad)
bad = Packetvoidruby::Mixed.new
bad.type = Packetvoidruby::MixedType::Payload
bad.payload.count = 40_000
expect_invalid("mixed-payload-range", bad)
bad = Packetvoidruby::Mixed.new
bad.type = Packetvoidruby::MixedType::Payload
bad.payload.marker = 1
expect_invalid("mixed-payload-bool", bad)
bad = Packetvoidruby::Tags.new
bad.type = 3
expect_invalid("tags-tag-3", bad)
bad = Packetvoidruby::Empty.new
bad.type = 1
expect_invalid("empty-tag", bad)

expect_refused("mixed-empty", Packetvoidruby::Mixed, "".b)
expect_refused("mixed-short-payload", Packetvoidruby::Mixed, [0xfe, 0xff].pack("C*"))
expect_refused("frame-empty", Packetvoidruby::Frame, "".b)
expect_refused("tags-tag-3", Packetvoidruby::Tags, "\x03".b)
expect_refused("single-empty", Packetvoidruby::Single, "".b)

fail_void("empty union wire") unless Packetvoidruby::Empty.new.write == "".b && Packetvoidruby::Empty.read("".b) == Packetvoidruby::Empty.new
puts "ok"
