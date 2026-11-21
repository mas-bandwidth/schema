# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'json-hostile').each do |row|
    case_name, unit, root, tree = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    begin
      text = RubyConformance.read_utf8_raw(File.join(tree, "#{root}.json"))
      value = cls.from_json(text)
    rescue pkg::Refused => e
      RubyConformance.raise_if_unsupported(e)
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    RubyConformance.write_bytes(outdir, "#{case_name}.wire", value.save)
    RubyConformance.write_bytes(outdir, case_name, ("read\n" + value.to_json).b)
  end
end
