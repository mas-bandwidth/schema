# frozen_string_literal: true

# The retain-refusal surface: one store through a read, a save, a refused read
# and a second save (test/conformance/README.md).
def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'retain-refusal') do |row|
    case_name, unit, root, first_wire, refused_wire = row[1, 5]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !pkg.const_defined?(:TableRetain, false) ||
       !cls.respond_to?(:load_retain) || !RubyConformance.supported?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    retain = pkg::TableRetain.new("\x00".b * (1 << 20), "\x00".b * (24 * (1 << 17)))
    begin
      value = cls.load_retain(RubyConformance.read_bytes(first_wire), retain, pkg::LoadReport.new)
    rescue pkg::Refused
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    RubyConformance.write_bytes(outdir, "#{case_name}.before", value.save_retain(retain, pkg::LoadReport.new))
    answer = +"read\n"
    begin
      cls.load_retain(RubyConformance.read_bytes(refused_wire), retain, pkg::LoadReport.new)
      answer << "read\n"
    rescue pkg::Refused
      answer << "refused\n"
    end
    # a store the refused read unbound from the first value may refuse the
    # save rather than write without its records
    begin
      after = value.save_retain(retain, pkg::LoadReport.new)
    rescue pkg::InvalidValue
      answer << "save-refused\n"
    else
      RubyConformance.write_bytes(outdir, "#{case_name}.wire", after)
      answer << "saved\n"
    end
    RubyConformance.write_bytes(outdir, case_name, answer.b)
  end
end
