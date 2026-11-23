# frozen_string_literal: true

# Check packet construction and serialization against the C++ default pins.
require_relative "packetdefaults"
require_relative "example"
require_relative "ludicrous"

def check(cond, name)
  raise "AssertionError: #{name}" unless cond
end

PINS_READ = []

def pin(name, value, klass)
  PINS_READ << name
  data = File.binread("#{name}.bin")
  check(value.write == data, "#{name} constructor wire")
  check(klass.read(data).write == data, "#{name} round trip")
end

# A fresh Value (defaults from constants) writes the pinned bytes, and the pin
# reads back to a fresh Value. Both constant schemas declare package arithmetic,
# so each is generated into its own tree (constant-<name>/) and removed from the
# runtime's constants and loaded features after the check.
def constant_pin(name)
  tree = File.join(__dir__, "constant-#{name}")
  check(File.directory?(tree), "the generated tree constant-#{name} exists")
  require File.join(tree, "arithmetic")
  value_class = Arithmetic::Value
  fresh = value_class.new
  data = File.binread("#{name}.bin")
  PINS_READ << name
  check(!data.empty?, "#{name} pin is not empty")
  check(fresh.write == data, "#{name} constructor wire")
  check(value_class.read(data) == fresh, "#{name} reads back to a fresh Value")
  check(value_class.read(data).write == data, "#{name} round trip")
ensure
  Object.send(:remove_const, :Arithmetic) if Object.const_defined?(:Arithmetic)
  $LOADED_FEATURES.reject! { |f| f.start_with?(tree) } if tree
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
constant_pin("hexadecimal-addition")
constant_pin("unsigned-subtraction")
# Every .bin handed to the driver must have been read: a pin the driver does not
# know fails loudly, never skipped.
bins = Dir.glob("*.bin").map { |f| File.basename(f, ".bin") }.sort
check(!bins.empty?, "the pins directory is not empty")
bins.each { |name| check(PINS_READ.include?(name), "pin #{name} was never read by the driver") }
puts "ok"
