# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'instance') do |row|
    case_name, unit, root, wire = row[1, 4]
    next if row.length > 5 && row[5] == 'no-text'

    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, "#{case_name}.json")
      next
    end
    value = cls.load(RubyConformance.read_bytes(wire))
    RubyConformance.write_bytes(outdir, "#{case_name}.json", value.to_json.b)
  end

  RubyConformance.cases(manifest, 'json-write-refusal') do |row|
    case_name, unit, root, wire, damage = row[1, 5]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, "#{case_name}.json")
      next
    end
    value = cls.load(RubyConformance.read_bytes(wire))
    if damage != 'none'
      form, field_name = damage.split(':', 2)
      raise ArgumentError, "unsupported text damage: #{damage}" unless form == 'narrow'

      value.public_send(:"#{field_name}=", "\xFF".b.force_encoding(::Encoding::UTF_8))
    end
    out = begin
      value.to_json.b
    rescue pkg::InvalidValue => e
      RubyConformance.raise_if_unsupported(e)
      "refused\n".b
    end
    RubyConformance.write_bytes(outdir, "#{case_name}.json", out)
  end
end
