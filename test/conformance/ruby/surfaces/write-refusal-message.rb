# frozen_string_literal: true

def self.run(manifest, outdir)
  connections = {}
  RubyConformance.entries(manifest, 'connection').each { |row| connections[row[1]] = row }
  RubyConformance.entries(manifest, 'write-refusal-message').each do |row|
    case_name, connection, root, wire, path, kind, token = row[1, 7]
    unit = connections[connection][2]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    has_msg = !pkg.nil? && pkg.respond_to?(:save_messages) && pkg.respond_to?(:measure_messages)
    if pkg.nil? || cls.nil? || !has_msg || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.absent(outdir, case_name, 'message form is not emitted for this root')
      next
    end
    value = cls.load(RubyConformance.read_bytes(wire))
    parts = path.tr('[', '.').delete(']').split('.').reject(&:empty?)
    owner = parts.length > 1 ? RubyConformance.target_node(value, parts[0...-1].join('.')) : value
    last = parts[-1]
    current = last.match?(/\A\d+\z/) ? owner[last.to_i] : owner.public_send(last.to_sym)
    converted = case kind
                when 'int', 'ordinal'
                  Integer(token, 10)
                when 'length'
                  len = Integer(token, 10)
                  is_bytes = current.nil? ? RubyConformance.field_is_bytes?(owner, last) : (current.encoding == ::Encoding::BINARY)
                  is_bytes ? ("\x00".b * len) : ('x' * len)
                when 'float'
                  RubyConformance.parse_float_token(token)
                when 'bytes'
                  raw = [token].pack('H*')
                  is_bytes = current.nil? ? RubyConformance.field_is_bytes?(owner, last) : (current.encoding == ::Encoding::BINARY)
                  is_bytes ? raw : raw.dup.force_encoding(::Encoding::UTF_8)
                when 'bool'
                  %w[true 1].include?(token.downcase)
                else
                  token
                end
    if last.match?(/\A\d+\z/)
      owner[last.to_i] = converted
    else
      owner.public_send(:"#{last}=", converted)
    end
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
