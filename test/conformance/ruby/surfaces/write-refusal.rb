# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'write-refusal') do |row|
    case_name, unit, root, wire, path, rule, token = row[1, 7]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.supported?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    # the base value and the mutation are the driver's own work: a failure
    # there is a driver error (RubyConformance.cases), never an answer
    value = cls.load(RubyConformance.read_bytes(wire))
    RubyConformance.assign_mutation(value, path, rule, token)
    begin
      value.measure
      value.save
      RubyConformance.write_bytes(outdir, case_name, "wrote\n".b)
    rescue pkg::InvalidValue
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
    end
  end
end
