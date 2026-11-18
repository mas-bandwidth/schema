# frozen_string_literal: true

# Check packet construction and serialization against the C++ default pins.
require_relative "packetdefaults"
require_relative "example"
require_relative "ludicrous"

def check(cond, name)
  raise "AssertionError: #{name}" unless cond
end

def pin(name, value, klass)
  data = File.binread("#{name}.bin")
  check(value.write == data, "#{name} constructor wire")
  check(klass.read(data).write == data, "#{name} round trip")
end

config = Example::ProbeConfig.new
check(config.retries == -1 && config.preferred == Example::Weapon::Railgun, "scalar and enum defaults")
fixed = Ludicrous::DegenerateProbe.new
check(fixed.locked_fixed == (-3 << 16) && fixed.locked_int == 7 && fixed.locked_wide == -12_345_678_901_234, "fixed and integer defaults")
sample = Packetdefaults::Sample.new
check(sample.name == "é𐀀" && sample.token == [0x5c, 0x6e, 0x5c, 0x74].pack("C*"), "constructor text bytes")
check(sample.name.frozen? && sample.token.frozen?, "constructor text bytes frozen")
check(sample.caps == (Packetdefaults::Caps::Jump | Packetdefaults::Caps::Fly), "constructor flags")
check(sample.empty_name == "" && sample.empty_token == "".b && sample.empty_caps == 0, "empty defaults")
pin("sample-defaults", sample, Packetdefaults::Sample)
empty = Packetdefaults::Sample.new
empty.name = ""
empty.token = "".b
empty.caps = 0
pin("sample-empty", empty, Packetdefaults::Sample)
short = Packetdefaults::Sample.new
short.name = "A"
short.token = "\x00\xff".b
short.caps = Packetdefaults::Caps::Crouch
pin("sample-short", short, Packetdefaults::Sample)
batch = Packetdefaults::Batch.new
check(batch.head == sample && batch.items.all? { |x| x == sample }, "fixed defaults")
check(batch.counted.length == 1 && batch.counted[0] == sample, "counted defaults")
pin("batch-defaults", batch, Packetdefaults::Batch)
zero = Packetdefaults::ZeroCount.new
check(zero.items.empty?, "zero count")
pin("zero-count", zero, Packetdefaults::ZeroCount)
on_val = Packetdefaults::Conditional.new
check(on_val.enabled && on_val.value == sample, "conditional defaults")
pin("conditional-on", on_val, Packetdefaults::Conditional)
off_val = Packetdefaults::Conditional.new
off_val.enabled = false
pin("conditional-off", off_val, Packetdefaults::Conditional)
choice = Packetdefaults::Choice.new
choice.type = Packetdefaults::ChoiceType::Sample
pin("choice-sample", choice, Packetdefaults::Choice)
puts "ok"
