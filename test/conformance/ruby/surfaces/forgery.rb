# frozen_string_literal: true

require_relative '_blocks'

def self.run(manifest, outdir)
  roots = {}
  RubyConformance.entries(manifest, 'block').each do |row|
    roots[row[1]] = [row[2], RubyConformanceBlocks.block_root(row[1])]
  end
  RubyConformance.entries(manifest, 'forgery').each do |row|
    name, kind, subject = row[1], row[2], row[3]
    next unless kind == 'block'

    unit, root = roots[subject]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.no_codec(outdir, name)
      next
    end
    data = RubyConformance.read_bytes(row[4])
    extent = Integer(row[5], 10)
    pointer = row[6]
    handle = if pointer == 'null'
               RubyConformanceBlocks.open_block(pkg, root, ''.b)
             else
               RubyConformanceBlocks.open_block(pkg, root, RubyConformanceBlocks.place(data, extent), Integer(pointer, 10))
             end
    RubyConformance.write_bytes(outdir, name, handle ? "open\n".b : "refuse\n".b)
  end
end
