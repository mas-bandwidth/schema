# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'framing').each do |row|
    case_name, unit, root, wire = row[1, 4]
    tree = if unit == 'tblr1'
             ENV['RUBY_FRAMING_GENERATED'] || File.join(RubyConformance::ROOT, 'tblr1', '_framing')
           end
    pkg = RubyConformance.unit_package(unit == 'tblr1' ? 'tblr1' : unit, tree)
    cls = RubyConformance.root_class(unit == 'tblr1' ? 'tblr1' : unit, root, tree)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    begin
      value = cls.load(RubyConformance.read_bytes(wire))
    rescue pkg::Refused => e
      RubyConformance.raise_if_unsupported(e)
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    text = value.to_json
    if unit == 'tblr1'
      fixed_pkg = RubyConformance.unit_package(unit)
      fixed_cls = RubyConformance.root_class(unit, root)
      if fixed_pkg.nil? || fixed_cls.nil? || !RubyConformance.fixed_carried?(fixed_pkg, fixed_cls)
        RubyConformance.no_codec(outdir, case_name)
        next
      end
      saved = fixed_cls.from_json(text).save
    else
      saved = value.save
    end
    RubyConformance.write_bytes(outdir, "#{case_name}.wire", saved)
    flags = row[5..] || []
    body = flags.include?('no-value') ? "read\n".b : ("read\n" + text).b
    RubyConformance.write_bytes(outdir, case_name, body)
  end
end
