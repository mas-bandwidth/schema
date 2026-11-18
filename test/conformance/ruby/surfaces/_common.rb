# frozen_string_literal: true

module RubyConformance
  ROOT = ENV['RUBY_CONFORMANCE_GENERATED'] || 'build/conformance-ruby'
  LOADED_UNITS = {}

  def self.entries(manifest, kind)
    out = []
    File.foreach(manifest, encoding: 'UTF-8') do |raw|
      text = raw.strip
      next if text.empty? || text.start_with?('#')

      fields = text.split
      out << fields if fields[0] == kind
    end
    out
  end

  def self.read_bytes(path)
    File.binread(path)
  end

  def self.read_utf8_raw(path)
    raw = File.binread(path)
    raw.dup.force_encoding(::Encoding::UTF_8)
  end

  def self.write_bytes(outdir, name, data)
    File.binwrite(File.join(outdir, name), data)
  end

  def self.absent(outdir, name, reason)
    write_bytes(outdir, "#{name}.absent", reason.to_s.b)
  end

  def self.module_const_name(pkg)
    pkg.split('_').map(&:capitalize).join
  end

  def self.unit_package(key, tree = nil)
    base = tree || ROOT
    cache_key = "#{base}::#{key}"
    return LOADED_UNITS[cache_key] if LOADED_UNITS.key?(cache_key)

    unitdir = File.expand_path(File.join(base, key))
    unless File.directory?(unitdir)
      LOADED_UNITS[cache_key] = nil
      return nil
    end

    Dir.children(unitdir).sort.each do |entry|
      next if entry.start_with?('_') || !entry.end_with?('.rb')

      pkg = entry.delete_suffix('.rb')
      next unless File.directory?(File.join(unitdir, pkg))

      require File.join(unitdir, entry)
      mod_name = module_const_name(pkg)
      mod = Object.const_defined?(mod_name, false) ? Object.const_get(mod_name, false) : nil
      LOADED_UNITS[cache_key] = mod
      return mod
    end

    LOADED_UNITS[cache_key] = nil
    nil
  end

  def self.root_class(unit, root, tree = nil)
    pkg = unit_package(unit, tree)
    return nil if pkg.nil?
    return nil unless pkg.const_defined?(root, false)

    pkg.const_get(root, false)
  end

  def self.fixed_carried?(pkg, cls)
    return true if cls.respond_to?(:descriptor) && cls.descriptor && cls.descriptor.variable

    pkg.respond_to?(:_s_fixed_load) ||
      (pkg.const_defined?(:SchemaTable, false) && pkg::SchemaTable.respond_to?(:fixed_load))
  end

  def self.supported?(pkg, cls)
    return false if pkg.nil? || cls.nil? || !cls.respond_to?(:descriptor) || cls.descriptor.nil?
    return false unless fixed_carried?(pkg, cls)

    seen = {}
    walk = nil
    carried = lambda do |field|
      return false if field.codec == 'unsupported'

      if %w[table pointer pointer_array].include?(field.codec) && field.target && !field.target.empty?
        return false unless pkg.const_defined?(field.target, false)

        target = pkg.const_get(field.target, false)
        return false if target.nil? || !target.respond_to?(:descriptor) || target.descriptor.nil?
        return false unless walk.call(target.descriptor)
      end
      true
    end

    walk = lambda do |desc|
      return true if seen[desc.name]

      seen[desc.name] = true
      desc.fields.each do |field|
        return false unless carried.call(field)

        elem = field.respond_to?(:elem) ? field.elem : nil
        return false if !elem.nil? && !carried.call(elem)
      end
      true
    end

    walk.call(cls.descriptor)
  end

  def self.raise_if_unsupported(err)
    reason = err.respond_to?(:reason) ? err.reason.to_s : ''
    rule = err.respond_to?(:rule) ? err.rule.to_s : ''
    if %w[unsupported unsupported_kind fixed_surface_not_selected].include?(reason) ||
       rule.include?('unsupported') || rule.include?('fixed_surface_not_selected')
      raise err
    end
  end

  def self.parse_float_token(token)
    case token.downcase
    when 'nan', '+nan', '-nan' then Float::NAN
    when 'inf', '+inf', 'infinity', '+infinity' then Float::INFINITY
    when '-inf', '-infinity' then -Float::INFINITY
    else Float(token)
    end
  end

  def self.deep_copy(obj)
    Marshal.load(Marshal.dump(obj))
  end

  def self.target_node(value, path)
    node = value
    path.tr('[', '.').delete(']').split('.').each do |part|
      next if part.empty?

      node = if part.match?(/\A\d+\z/)
               node[part.to_i]
             else
               node.public_send(part.to_sym)
             end
    end
    node
  end

  def self.field_is_bytes?(owner, name)
    return false unless owner.class.respond_to?(:descriptor) && owner.class.descriptor

    fd = owner.class.descriptor.fields.find { |f| f.name == name }
    return false if fd.nil?

    inner = fd.codec == 'optional' && fd.elem ? fd.elem : fd
    %w[bytes bytes_blob].include?(inner.codec)
  end

  def self.assign_mutation(value, path, rule, token)
    parts = path.tr('[', '.').delete(']').split('.').reject(&:empty?)
    owner = parts.length > 1 ? target_node(value, parts[0...-1].join('.')) : value
    name = parts[-1]
    current = owner.public_send(name.to_sym)
    case rule
    when 'count'
      count = Integer(token, 10)
      if count.negative? || !current.is_a?(::Array) || (count.positive? && current.empty?)
        raise ArgumentError, 'count mutation needs a nonnegative count and a populated list'
      end
      resized = current[0, count] || []
      resized << deep_copy(current[0]) while resized.length < count
      owner.public_send(:"#{name}=", resized)
    when 'length'
      length = Integer(token, 10)
      raise ArgumentError, 'length cannot be negative' if length.negative?

      is_bytes = current.nil? ? field_is_bytes?(owner, name) : (current.encoding == ::Encoding::BINARY)
      owner.public_send(:"#{name}=", is_bytes ? ('a'.b * length) : ('a' * length))
    when 'ordinal', 'int'
      owner.public_send(:"#{name}=", Integer(token, 10))
    when 'tag'
      union_val = current.class.new
      union_val.type = Integer(token, 10)
      owner.public_send(:"#{name}=", union_val)
    when 'bool'
      owner.public_send(:"#{name}=", %w[true 1].include?(token.downcase))
    when 'float'
      owner.public_send(:"#{name}=", parse_float_token(token))
    when 'bytes'
      raw = [token].pack('H*')
      is_bytes = current.nil? ? field_is_bytes?(owner, name) : (current.encoding == ::Encoding::BINARY)
      val = is_bytes ? raw : raw.dup.force_encoding(::Encoding::UTF_8)
      owner.public_send(:"#{name}=", val)
    else
      raise ArgumentError, "unknown write-refusal mutation: #{rule}"
    end
  end
end
