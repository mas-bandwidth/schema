# frozen_string_literal: true

# THE BLOCK BUILDER holds the write contract (SPEC-TABLES §19.1, SF-31):
# set one field on the root's value and build its block through
# cls.block_begin_from(value); `wrote` when it completes, `refused` when it
# raises InvalidValue.
def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'write-refusal-block').each do |row|
    case_name, unit, root, wire, path, rule, token = row[1, 7]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls) || !cls.respond_to?(:block_begin_from)
      RubyConformance.absent(outdir, case_name, 'unit not generated for ruby')
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
      cls.block_begin_from(value)
      "wrote\n".b
    rescue pkg::InvalidValue
      "refused\n".b
    end
    RubyConformance.write_bytes(outdir, case_name, out)
  end
end
