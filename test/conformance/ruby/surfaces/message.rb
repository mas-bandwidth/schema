# frozen_string_literal: true

def self.run(manifest, outdir)
  connections = {}
  RubyConformance.entries(manifest, 'connection').each { |row| connections[row[1]] = row }
  RubyConformance.cases(manifest, 'message') do |row|
    case_name, connection, root, _, wire = row[1, 5]
    conn = connections[connection]
    pkg = RubyConformance.unit_package(conn[2])
    cls = RubyConformance.root_class(conn[2], root)
    has_msg = !pkg.nil? && pkg.respond_to?(:load_messages) && pkg.const_defined?(:MessageVocabulary, false)
    # NO CODEC FIRST, whatever the row owes: a unit, root or message form this
    # leg does not generate is an absence, on a must-refuse row as on any other
    if pkg.nil? || cls.nil? || !has_msg
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    if wire == '-'
      # a present codec is held to the whole inapplicability check
      raise "#{case_name}: root unexpectedly carries message form" if pkg::SchemaTable::MSG_ROOTS[root]

      refused_write = false
      begin
        pkg.save_messages(cls, [cls.new])
      rescue pkg::InvalidValue => e
        raise unless e.rule == 'message root or vocabulary is invalid'

        refused_write = true
      end
      raise "#{case_name}: message writer accepted an inapplicable root" unless refused_write

      refused_read = false
      begin
        pkg.load_messages(cls, pkg::MessageVocabulary.new, ''.b)
      rescue pkg::Refused => e
        raise unless e.reason == 'invalid_root'

        refused_read = true
      end
      raise "#{case_name}: message reader accepted an inapplicable root" unless refused_read

      RubyConformance.absent(outdir, case_name, 'message form is inapplicable to this variable root')
      next
    end
    vocab = pkg::MessageVocabulary.new
    begin
      pkg.announce_read(vocab, RubyConformance.read_bytes(conn[4]))
      raw = RubyConformance.read_bytes(wire)
      values = pkg.load_messages(cls, vocab, raw, [raw.bytesize, 256].max)
    rescue pkg::Refused => e
      # the present codec's public refusal is its answer (the verdict, never
      # the reason, is compared)
      RubyConformance.absent(outdir, case_name, e.reason)
      next
    end
    RubyConformance.write_bytes(outdir, case_name, pkg.save_messages(cls, values))
  end
end
