# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'write-refusal-json') do |row|
    case_name, unit, root, wire, path, rule, token = row[1, 7]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    value = RubyConformance.load_or_fresh(cls, wire)
    begin
      RubyConformance.assign_mutation(value, path, rule, token)
    rescue RubyConformance::Unrepresentable
      RubyConformance.write_bytes(outdir, case_name, "unrepresentable\n".b)
      next
    end
    out = begin
      value.to_json
      "wrote\n".b
    rescue pkg::InvalidValue => e
      RubyConformance.raise_if_unsupported(e)
      "refused\n".b
    end
    RubyConformance.write_bytes(outdir, case_name, out)
  end
end
