# frozen_string_literal: true

def self.run(manifest, outdir)
  connections = {}
  RubyConformance.entries(manifest, 'connection').each { |row| connections[row[1]] = row }
  RubyConformance.cases(manifest, 'write-refusal-message') do |row|
    case_name, connection, root, wire, path, kind, token = row[1, 7]
    unit = connections[connection][2]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    has_msg = !pkg.nil? && pkg.respond_to?(:save_messages) && pkg.respond_to?(:measure_messages)
    if pkg.nil? || cls.nil? || !has_msg || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    value = cls.load(RubyConformance.read_bytes(wire))
    RubyConformance.assign_mutation(value, path, kind, token)
    refused = [:measure_messages, :save_messages].map do |verb|
      pkg.public_send(verb, cls, [value])
      false
    rescue pkg::InvalidValue
      true
    end
    raise 'message measure and save disagree' if refused[0] != refused[1]

    RubyConformance.write_bytes(outdir, case_name, refused[0] ? "refused\n".b : "wrote\n".b)
  end
end
