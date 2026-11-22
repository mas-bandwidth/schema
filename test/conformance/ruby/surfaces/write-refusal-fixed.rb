# frozen_string_literal: true

def self.default_for_spec(pkg, spec)
  shape = spec['shape']
  if %w[table union].include?(shape) && spec['target']
    cls = pkg._s_fixed_classes[spec['target']]
    return cls.new if cls
  end
  return false if shape == 'bool'
  return 0.0 if shape == 'float'
  return '' if shape == 'string'
  return ''.b if shape == 'bytes'
  return [] if shape == 'array'

  spec['default'] || 0
end

def self.assign_fixed(pkg, root_val, spec, path, rule, token)
  parts = path.tr('[', '.').delete(']').split('.').reject(&:empty?)
  owner = root_val
  current = spec
  parts[0...-1].each do |part|
    if part.match?(/\A\d+\z/)
      owner = owner[part.to_i]
      current = current['children'][0]
    else
      field = current['children'].find { |f| f['name'] == part }
      owner = owner.public_send(part.to_sym)
      current = field
      current = current['children'][0] if current['shape'] == 'optional'
    end
  end
  name = parts[-1]
  if name.match?(/\A\d+\z/)
    idx = name.to_i
    idx += 1 if owner.class.name.end_with?('KeyedArray')
    field = current['children'][0]
    previous = owner[idx]
    assigned = case rule
               when 'int', 'ordinal' then Integer(token, 10)
               when 'float' then RubyConformance.parse_float_token(token)
               when 'length' then field['shape'] == 'string' ? ('x' * Integer(token, 10)) : ("\x00".b * Integer(token, 10))
               when 'bytes'
                 raw = [token].pack('H*')
                 field['shape'] == 'string' ? raw.dup.force_encoding(::Encoding::UTF_8) : raw
               else raise ArgumentError, "unknown indexed fixed-write recipe #{rule}"
               end
    owner[idx] = assigned
    return
  end
  field = current['children'].find { |f| f['name'] == name }
  previous = owner.public_send(name.to_sym)
  case rule
  when 'int', 'ordinal'
    assigned = Integer(token, 10)
  when 'float'
    assigned = RubyConformance.parse_float_token(token)
  when 'tag'
    previous.type = Integer(token, 10)
    return
  when 'count'
    count = Integer(token, 10)
    assigned = (previous[0, count] || []).dup
    assigned << default_for_spec(pkg, field['children'][0]) while assigned.length < count
  when 'length'
    count = Integer(token, 10)
    assigned = field['shape'] == 'string' ? ('x' * count) : ("\x00".b * count)
  when 'bytes'
    raw = [token].pack('H*')
    assigned = field['shape'] == 'string' ? raw.dup.force_encoding(::Encoding::UTF_8) : raw
  else
    raise ArgumentError, "unknown fixed-write recipe #{rule}"
  end
  owner.public_send(:"#{name}=", assigned)
end

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'write-refusal-fixed') do |row|
    case_name, unit, root, wire, path, rule, token = row[1, 7]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !cls.respond_to?(:fixed_save) || !pkg.respond_to?(:_s_fixed_roots)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    # A committed wire the reader refuses is a reader failure, never a missing
    # feature: let it raise, so the surface is red and not absent.
    value = cls.load(RubyConformance.read_bytes(wire))
    # only the save may refuse: an InvalidValue raised while assigning the
    # mutation is a driver failure, never the verdict under test
    assign_fixed(pkg, value, pkg._s_fixed_roots[root]['spec'], path, rule, token)
    begin
      cls.fixed_save([value])
      RubyConformance.write_bytes(outdir, case_name, "wrote\n".b)
    rescue pkg::InvalidValue
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
    end
  end
end
