# frozen_string_literal: true

# THE COOK WRITER holds the write contract (SPEC-TABLES §7.6; README, the
# 2026-11-22 surfaces): the row's value through CookMeasure, then Cook in
# little-endian order into exactly the measured capacity; `wrote` when both
# complete, `refused` when either raises InvalidValue.
def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'write-refusal-cook').each do |row|
    case_name, unit, root, wire, path, rule, token = row[1, 7]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
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
      size = pkg::SchemaTable.cook_measure(value)
      cooked = pkg::SchemaTable.cook_table(value, order: pkg::SchemaTable::COOK_ORDER_LITTLE, capacity: size)
      raise "#{case_name}: cook measure #{size} and write #{cooked.bytesize} disagree" unless cooked.bytesize == size

      "wrote\n".b
    rescue pkg::InvalidValue
      "refused\n".b
    end
    RubyConformance.write_bytes(outdir, case_name, out)
  end
end
