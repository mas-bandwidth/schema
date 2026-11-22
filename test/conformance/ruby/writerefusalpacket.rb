# frozen_string_literal: true

# The write-refusal-packet probe for the Ruby leg (docs/SPEC.md section 5.1,
# test/conformance/write-refusal-packet/rows.txt). It builds a fresh lawful
# PacketWriteRefusal, applies ONE op from each row, calls the generated packet
# write and prints "<name> refused" or "<name> wrote". The Ruby packet writer
# returns the bytes it wrote (it sizes its own stream) and refuses with
# Packetrefusal::InvalidValue, the one error this probe catches: any other
# error is a crash, not a refusal, and stops the probe.

require_relative "packetrefusal"

def apply(value, op, arg)
  case op
  when "scalar" then value.scalar = Integer(arg)
  when "narrow" then value.narrow = Integer(arg)
  when "grade" then value.grade = Integer(arg)
  when "shot_tag" then value.shot.type = Integer(arg)
  when "window_count" then value.window = ::Array.new(Integer(arg), 0)
  when "text_length" then value.text = "a" * Integer(arg)
  when "text_bytes" then value.text = [arg].pack("H*").force_encoding(Encoding::UTF_8)
  when "text_fill" then value.text = "a" * Integer(arg)
  when "fixed_constant" then value.fixed_constant = Integer(arg)
  when "reserved_zero" then value.reserved_zero = Integer(arg)
  else raise ArgumentError, "unknown op #{op}"
  end
end

File.readlines(ARGV.fetch(0), chomp: true).each do |line|
  line = line.strip
  next if line.empty? || line.start_with?("#")

  name, _verdict, op, arg = line.split
  value = Packetrefusal::PacketWriteRefusal.new
  apply(value, op, arg)
  begin
    bytes = value.write
    raise "#{name}: write returned #{bytes.class}, not a String" unless bytes.is_a?(String)
    puts "#{name} wrote"
  rescue Packetrefusal::InvalidValue
    puts "#{name} refused"
  end
end
