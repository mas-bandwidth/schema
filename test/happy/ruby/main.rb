# frozen_string_literal: true

# The per-PR happy path for Ruby: one ShipCreate written with the generated
# packet writer, read back with the generated reader, every field compared.
# Run by make/happy-path-ruby.mk with the generated directory on -I.
require "happy"

def fail!(why)
  warn "happy ruby: FAILED: #{why}"
  exit 1
end

sent = Happy::ShipCreate.new
sent.ship_type = Happy::ShipType::Corvette
sent.position.x = 1.5
sent.position.y = -2.25
sent.position.z = 3.0
sent.health = 777
sent.alive = true

begin
  data = sent.write
rescue StandardError => e
  fail!("write: #{e.class}: #{e.message}")
end
begin
  got = Happy::ShipCreate.read(data)
rescue StandardError => e
  fail!("read: #{e.class}: #{e.message}")
end

[
  ["ship_type", got.ship_type, Happy::ShipType::Corvette],
  ["position.x", got.position.x, 1.5],
  ["position.y", got.position.y, -2.25],
  ["position.z", got.position.z, 3.0],
  ["health", got.health, 777],
  ["alive", got.alive, true]
].each do |name, have, want|
  fail!("#{name}: got #{have.inspect}, want #{want.inspect}") unless have.eql?(want)
end

puts "happy ruby: ok (#{data.bytesize} bytes)"
