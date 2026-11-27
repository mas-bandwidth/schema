# frozen_string_literal: true

# The Ruby leg of test/packet-void (SPEC §4.8): answers every case of
# testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
# line each, for tools/casegate (and internal/casegate in the Go tests). The
# pins are the Go oracle's bytes, never this writer's.
#
#   ruby main.rb <generated directory> <testdata/wire/packet-void>
require File.expand_path("packetvoid", ARGV.fetch(0))

PINS = ARGV.fetch(1)

TYPES = {
  "Mixed" => Packetvoid::Mixed,
  "Late" => Packetvoid::Late,
  "Tags" => Packetvoid::Tags,
  "Single" => Packetvoid::Single,
  "Empty" => Packetvoid::Empty,
  "Frame" => Packetvoid::Frame,
  "Batch" => Packetvoid::Batch
}.freeze

def mixed(tag)
  value = Packetvoid::Mixed.new
  value.type = tag
  value
end

# The value of each accept case, by name; nil for a name this driver does not
# know, which is FAILED and never written with another case's value.
def build_case(name)
  case name
  when "mixed-none" then mixed(Packetvoid::MixedType::None)
  when "mixed-ping" then mixed(Packetvoid::MixedType::Ping)
  when "mixed-payload" then mixed(Packetvoid::MixedType::Payload)
  when "late-payload", "late-pong"
    v = Packetvoid::Late.new
    v.type = name == "late-payload" ? Packetvoid::LateType::Payload : Packetvoid::LateType::Pong
    v
  when "tags-none", "tags-ping", "tags-pong"
    v = Packetvoid::Tags.new
    v.type = { "tags-none" => Packetvoid::TagsType::None, "tags-ping" => Packetvoid::TagsType::Ping,
               "tags-pong" => Packetvoid::TagsType::Pong }.fetch(name)
    v
  when "single-none", "single-ack"
    v = Packetvoid::Single.new
    v.type = name == "single-ack" ? Packetvoid::SingleType::Ack : Packetvoid::SingleType::None
    v
  when "empty-none" then Packetvoid::Empty.new
  when "frame-ping"
    v = Packetvoid::Frame.new
    v.lead = 5
    v.choice = mixed(Packetvoid::MixedType::Ping)
    v.tail = 6
    v
  when "batch-empty", "batch-ping", "batch-ping-payload"
    v = Packetvoid::Batch.new
    v.values = { "batch-empty" => [], "batch-ping" => [mixed(Packetvoid::MixedType::Ping)],
                 "batch-ping-payload" => [mixed(Packetvoid::MixedType::Ping), mixed(Packetvoid::MixedType::Payload)] }.fetch(name)
    v
  end
end

# The type's default value with the one change each refuse-write case names;
# nil for a name this driver does not know, which is FAILED. Ruby holds every
# change, the text tag and the Integer payload included.
def build_refused(name)
  case name
  when "mixed-write-tag-negative" then mixed(-1)
  when "mixed-write-tag-text" then mixed("ping")
  when "mixed-write-tag-3" then mixed(3)
  when "mixed-write-payload-type"
    v = mixed(Packetvoid::MixedType::Payload)
    v.payload = 1
    v
  when "mixed-write-marker-8"
    v = mixed(Packetvoid::MixedType::Payload)
    v.payload.marker = 8
    v
  when "tags-write-tag-3"
    v = Packetvoid::Tags.new
    v.type = 3
    v
  when "empty-write-tag-1"
    v = Packetvoid::Empty.new
    v.type = 1
    v
  end
end

# Reads mixed-ping's pin, then selects the payload arm the Ruby API's way on
# the value read: set the tag alone. The union keeps its payload object, so
# the write shows what the read of the void arm left there.
def select_payload(pin, bits)
  value = Packetvoid::Mixed.read(File.binread(File.join(PINS, "mixed-ping.bin")))
  return "read mixed-ping: tag #{value.type.inspect}" if value.type != Packetvoid::MixedType::Ping

  value.type = Packetvoid::MixedType::Payload
  got = value.write
  return "write #{got.unpack1("H*")}, pin #{pin.unpack1("H*")}" if got != pin

  _, read_bits = Packetvoid::Mixed.read_bits(got)
  return "written value reads #{read_bits} bits, want #{bits}" if read_bits != Integer(bits, 10)

  nil
rescue Packetvoid::Refused => e
  "read mixed-ping refused: #{e.message}"
end

def answer(name, type_name, verdict, bits)
  klass = TYPES[type_name]
  return "unknown type #{type_name}" if klass.nil?

  if verdict == "refuse-write"
    bad = build_refused(name)
    return "unknown refuse-write case" if bad.nil?
    return "case type #{type_name}, the driver builds another" unless bad.instance_of?(klass)

    begin
      got = bad.write
    rescue Packetvoid::InvalidValue
      return nil
    rescue StandardError => e
      return "write raised #{e.class}: #{e.message}, want Packetvoid::InvalidValue"
    end
    return "write accepted: #{got.unpack1("H*")}"
  end

  pin = File.binread(File.join(PINS, "#{name}.bin"))
  if verdict == "refuse"
    begin
      klass.read(pin)
    rescue Packetvoid::Refused
      return nil
    end
    return "read accepted"
  end
  if verdict == "select"
    return "unknown select case" unless name == "mixed-ping-select-payload"

    return select_payload(pin, bits)
  end
  return "verdict #{verdict}" unless verdict == "accept"

  value = build_case(name)
  return "unknown accept case" if value.nil?
  return "case type #{type_name}, the driver builds another" unless value.instance_of?(klass)

  got = value.write
  return "write #{got.unpack1("H*")}, pin #{pin.unpack1("H*")}" if got != pin

  begin
    back, read_bits = klass.read_bits(pin)
  rescue Packetvoid::Refused => e
    return "read refused: #{e.message}"
  end
  return "read has #{read_bits} bits, want #{bits}" if read_bits != Integer(bits, 10)
  return "read value differs: #{back.inspect}" if back != value
  return "write what was read differs" if back.write != pin

  begin
    klass.read(pin)
  rescue Packetvoid::Refused => e
    return "one-packet read refused: #{e.message}"
  end
  nil
end

File.readlines(File.join(PINS, "cases.txt")).each do |line|
  fields = line.split
  next if fields.empty? || fields[0].start_with?("#")

  if fields.length < 4
    puts "case #{fields[0]} FAILED: a case line needs <name> <type> <verdict> <bits>"
    next
  end
  why = answer(fields[0], fields[1], fields[2], fields[3])
  puts(why ? "case #{fields[0]} FAILED: #{why}" : "case #{fields[0]} ok")
end
