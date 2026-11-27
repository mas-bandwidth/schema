# frozen_string_literal: true

# The write-refusal-shape surface (DW-TABLE-SHAPE; FORMAT.md,
# `write-refusal-shape`): a value of the WRONG KIND put where the row's path
# names, then the root's own writer, measure and save.
#
# Ruby refuses in every build: a write refusal is the unit's InvalidValue. Any
# other exception is no refusal: it is answered `raised`, which is red against
# either verdict. Ruby's values hold every token but one: there is no improper
# list, so `improper` is `unrepresentable`. A wrong whole value is handed to
# the writer the root's save calls (the fixed form's _s_fixed_save, or
# SchemaTable.measure_table and save_table), since it has no save of its own.
# The lawful twins (an ordinary kind) are set and written exactly as
# write-refusal writes them.

SHAPE_TOKENS = {
  'int' => -> { 1 },
  'float' => -> { 1.5 },
  'numtext' => -> { '1.5' },
  'text' => -> { 'x' },
  'bool' => -> { true },
  'null' => -> { nil },
  'map' => -> { {} },
  'seq' => -> { [1] }
}.freeze

# shape_place puts wrong where path names. `f[i]` makes the array f its first
# i elements as loaded, then wrong; a last part naming an arm of a union
# selects that arm with wrong as its payload.
def self.shape_place(value, path, wrong)
  parts = path.tr('[', '.').delete(']').split('.').reject(&:empty?)
  if parts[-1].match?(/\A\d+\z/)
    owner = parts.length > 2 ? RubyConformance.target_node(value, parts[0...-2].join('.')) : value
    name = parts[-2]
    items = owner.public_send(name.to_sym).to_a.first(parts[-1].to_i)
    items << wrong
    owner.public_send(:"#{name}=", items)
    return
  end
  owner = parts.length > 1 ? RubyConformance.target_node(value, parts[0...-1].join('.')) : value
  name = parts[-1]
  descriptor = owner.class.respond_to?(:descriptor) ? owner.class.descriptor : nil
  arms = descriptor.respond_to?(:arms) ? descriptor.arms : nil
  arm = arms&.find { |a| a.name == name }
  owner.type = arm.tag if arm
  owner.public_send(:"#{name}=", wrong)
end

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'write-refusal-shape') do |row|
    case_name, unit, root, wire, path, rule, token = row[1, 7]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.supported?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    value = RubyConformance.load_or_fresh(cls, wire)
    if rule != 'shape'
      begin
        RubyConformance.assign_mutation(value, path, rule, token)
      rescue RubyConformance::Unrepresentable
        RubyConformance.write_bytes(outdir, case_name, "unrepresentable\n".b)
        next
      end
    elsif !SHAPE_TOKENS.key?(token)
      RubyConformance.write_bytes(outdir, case_name, "unrepresentable\n".b)
      next
    elsif path == '.'
      value = SHAPE_TOKENS[token].call
    else
      shape_place(value, path, SHAPE_TOKENS[token].call)
    end
    answer = begin
      if value.is_a?(cls)
        value.measure
        value.save
      elsif cls.descriptor.fixed
        pkg._s_fixed_save(root, cls, [value])
      else
        pkg::SchemaTable.measure_table(value)
        pkg::SchemaTable.save_table(value)
      end
      "wrote\n"
    rescue pkg::InvalidValue
      "refused\n"
    rescue StandardError
      # any other raise is no refusal, and red
      "raised\n"
    end
    RubyConformance.write_bytes(outdir, case_name, answer.b)
  end
end
