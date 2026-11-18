# frozen_string_literal: true

module RubyConformanceCooks
  def self.open_cook(pkg, root_name, data, base_offset = 0)
    cls = pkg.const_get(root_name, false)
    pkg::SchemaTable.open_cook(cls, data, base_offset: base_offset)
  rescue StandardError => e
    raise unless e.is_a?(pkg::Refused)

    nil
  end
end
