# frozen_string_literal: true

def self.run(manifest, outdir)
  instances = {}
  RubyConformance.entries(manifest, 'instance').each { |row| instances[row[1]] = row }
  RubyConformance.entries(manifest, 'cook-write').each do |row|
    name, text, source, alias_name = row[1, 4]
    instance = instances[name]
    pkg = RubyConformance.unit_package(instance[2])
    cls = RubyConformance.root_class(instance[2], instance[3])
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, name)
      RubyConformance.no_codec(outdir, "#{name}-be")
      next
    end
    value = if text == '-'
              cls.load(RubyConformance.read_bytes(instance[4]))
            else
              cls.from_json(RubyConformance.read_utf8_raw(text))
            end
    if source != '-'
      body = value.public_send(source.to_sym)
      shared = pkg::BlobRef.new(body)
      value.public_send(:"#{source}=", shared)
      value.public_send(:"#{alias_name}=", shared)
    end
    size = pkg::SchemaTable.cook_measure(value)
    [[1, ''], [2, '-be']].each do |order, suffix|
      refused_short = false
      begin
        pkg::SchemaTable.cook_table(value, order: order, capacity: size - 1)
      rescue pkg::InvalidValue
        refused_short = true
      end
      raise "#{name}: short capacity did not refuse" unless refused_short

      result = pkg::SchemaTable.cook_table(value, order: order, capacity: size)
      raise "#{name}: measure and write disagree" unless result.bytesize == size

      RubyConformance.write_bytes(outdir, name + suffix, result)
    end
  end
end
