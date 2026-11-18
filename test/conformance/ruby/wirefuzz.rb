# frozen_string_literal: true

# The generated Ruby table codecs on the conformance verdict pipe.

module RubyWireFuzz
  GENERATED = File.expand_path(ARGV[0] || 'build/conformance-ruby')
  UNITS = {}

  def self.exact(io, size)
    return ''.b if size.zero?

    data = io.read(size)
    raise EOFError, 'truncated wire-fuzz frame' if data.nil? || data.bytesize != size

    data
  end

  def self.read_name(io)
    size = exact(io, 2).unpack1('v')
    raw = exact(io, size)
    raw.force_encoding(::Encoding::UTF_8)
  end

  def self.module_const_name(pkg)
    pkg.split('_').map(&:capitalize).join
  end

  def self.load_unit(key)
    return UNITS[key] if UNITS.key?(key)

    raise ArgumentError, "invalid unit key #{key}" unless key.match?(/\A[A-Za-z0-9_-]+\z/)

    directory = File.join(GENERATED, key)
    unless File.directory?(directory)
      UNITS[key] = nil
      return nil
    end

    entries = Dir.children(directory).sort.select do |n|
      !n.start_with?('_') && n.end_with?('.rb') && File.directory?(File.join(directory, n.delete_suffix('.rb')))
    end
    raise "expected one generated package for #{key}" unless entries.length == 1

    pkg = entries[0].delete_suffix('.rb')
    require File.join(directory, entries[0])
    mod_name = module_const_name(pkg)
    mod = Object.const_defined?(mod_name, false) ? Object.const_get(mod_name, false) : nil
    UNITS[key] = mod
    mod
  end

  def self.fixed_carried?(mod)
    mod.respond_to?(:_s_fixed_load) ||
      (mod.const_defined?(:SchemaTable, false) && mod::SchemaTable.respond_to?(:fixed_load))
  end

  def self.run
    $stdin.binmode
    $stdout.binmode
    header = $stdin.read(4)
    return if header.nil? || header.empty?
    raise EOFError, 'truncated roster count' unless header.bytesize == 4

    count = header.unpack1('V')
    roster = Array.new(count) do
      key = read_name($stdin)
      root = read_name($stdin)
      form, retain_byte = exact($stdin, 2).bytes
      retain = retain_byte != 0
      mod = load_unit(key)
      cls = mod && mod.const_defined?(root, false) ? mod.const_get(root, false) : nil
      cls = nil if cls && (!cls.respond_to?(:descriptor) || cls.descriptor.nil?)
      if form == 2
        if mod.nil? || cls.nil? || !mod.respond_to?(:load_messages) || !mod::SchemaTable::MSG_ROOTS[root] || !cls.respond_to?(:fixed_load)
          cls = nil
        end
      elsif form == 1
        if cls && !cls.descriptor.variable && !fixed_carried?(mod)
          cls = nil
        end
      else
        cls = nil
      end
      if retain && (form != 1 || cls.nil? || !cls.descriptor.variable ||
                    !mod.const_defined?(:TableRetain, false) || !cls.respond_to?(:load_retain))
        cls = nil
      end
      [mod, cls, form, retain]
    end

    $stdout.write(roster.map { |_, cls, _, _| cls.nil? ? 0 : 1 }.pack('C*'))
    $stdout.flush

    loop do
      mhdr = $stdin.read(8)
      return if mhdr.nil? || mhdr.empty?
      raise EOFError, 'truncated mutant header' unless mhdr.bytesize == 8

      index, size = mhdr.unpack('VV')
      data = exact($stdin, size)
      mod, cls, form, retain = roster[index]
      raise 'mutant for absent roster entry' if cls.nil?

      report = mod::LoadReport.new
      loaded = false
      refused = false
      saved = nil
      retained = nil
      value = nil

      begin
        if retain
          retained = mod::TableRetain.new("\x00".b * ((64 * data.bytesize) + 4096), "\x00".b * (24 * 4096))
          value = cls.load_retain(data, retained, report)
        elsif form == 2
          vocab = mod::MessageVocabulary.new
          mod.announce_read(vocab, mod::ANNOUNCEMENT)
          value = mod.load_messages(cls, vocab, data, [data.bytesize, 256].max, report)
        elsif cls.respond_to?(:fixed_load)
          value = cls.fixed_load(data, capacity: data.bytesize / 9, report: report)
        else
          value = cls.load(data, report)
        end
        loaded = true
      rescue mod::Refused
        refused = true
      end

      if loaded
        begin
          saved = if retain
                    value.save_retain(retained, report)
                  elsif form == 2
                    mod.save_messages(cls, value)
                  elsif cls.respond_to?(:fixed_load)
                    cls.fixed_save(value)
                  else
                    value.save
                  end
        rescue mod::InvalidValue
          saved = nil
        end
      end

      out_hdr = [
        loaded ? 1 : 0,
        report.unknown, 0, report.widened, 0, 0,
        0, refused ? 1 : 0,
        -1,
        report.retained, report.retain_lost,
        saved ? saved.bytesize : -1
      ].pack('Cl<5CCq<l<2q<')
      $stdout.write(out_hdr)
      $stdout.write(saved) if saved
      $stdout.flush
    end
  end
end

RubyWireFuzz.run
