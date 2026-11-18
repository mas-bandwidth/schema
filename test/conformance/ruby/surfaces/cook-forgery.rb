# frozen_string_literal: true

require_relative '_blocks'
require_relative '_cooks'

def self.run(manifest, outdir)
  roots = {}
  RubyConformance.entries(manifest, 'cook').each do |row|
    roots[row[1]] = [row[2], row[3]]
  end
  RubyConformance.entries(manifest, 'forgery').each do |row|
    name, kind, subject = row[1, 3]
    next unless kind == 'cook'

    unit, root = roots[subject]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.absent(outdir, name, 'unit not generated for ruby')
      next
    end
    data = RubyConformance.read_bytes(row[4])
    extent = Integer(row[5], 10)
    pointer = row[6]
    handle = if pointer == 'null'
               RubyConformanceCooks.open_cook(pkg, root, ''.b)
             else
               RubyConformanceCooks.open_cook(pkg, root, RubyConformanceBlocks.place(data, extent), Integer(pointer, 10))
             end
    RubyConformance.write_bytes(outdir, name, handle ? "open\n".b : "refuse\n".b)
  end
end
