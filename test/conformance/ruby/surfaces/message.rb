# frozen_string_literal: true

def self.run(manifest, outdir)
  connections = {}
  RubyConformance.entries(manifest, 'connection').each { |row| connections[row[1]] = row }
  RubyConformance.entries(manifest, 'message').each do |row|
    case_name, connection, root, _, wire = row[1, 5]
    conn = connections[connection]
    pkg = RubyConformance.unit_package(conn[2])
    cls = RubyConformance.root_class(conn[2], root)
    has_msg = !pkg.nil? && pkg.respond_to?(:load_messages) && pkg.const_defined?(:MessageVocabulary, false)
    if wire == '-'
      raise "#{case_name}: cannot verify message inapplicability" if pkg.nil? || cls.nil? || !has_msg
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

      # inapplicable to this variable root: a must-refuse row, answered
      # `refused` (ruling R13: an absence is never credited)
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    if pkg.nil? || cls.nil? || !has_msg
      RubyConformance.absent(outdir, case_name, 'message form is not emitted for this root')
      next
    end
    vocab = pkg::MessageVocabulary.new
    pkg.announce_read(vocab, RubyConformance.read_bytes(conn[4]))
    raw = RubyConformance.read_bytes(wire)
    values = pkg.load_messages(cls, vocab, raw, [raw.bytesize, 256].max)
    RubyConformance.write_bytes(outdir, case_name, pkg.save_messages(cls, values))
  end
  # message-batch <case> <connection> <root> <wire>: load with room for every
  # body and answer the count load_messages returned (docs/SPEC-TABLES.md
  # §3.3; FORMAT.md, `message-batch`). The Ruby read raises Refused rather
  # than returning a count, so a refused batch counts 0.
  RubyConformance.entries(manifest, 'message-batch').each do |row|
    case_name, connection, root, wire = row[1, 4]
    conn = connections[connection]
    pkg = RubyConformance.unit_package(conn[2])
    cls = RubyConformance.root_class(conn[2], root)
    has_msg = !pkg.nil? && pkg.respond_to?(:load_messages) && pkg.const_defined?(:MessageVocabulary, false)
    if pkg.nil? || cls.nil? || !has_msg
      RubyConformance.absent(outdir, case_name, 'message form is not emitted for this root')
      next
    end
    vocab = pkg::MessageVocabulary.new
    pkg.announce_read(vocab, RubyConformance.read_bytes(conn[4]))
    count =
      begin
        pkg.load_messages(cls, vocab, RubyConformance.read_bytes(wire), 256).length
      rescue pkg::Refused
        0
      end
    RubyConformance.write_bytes(outdir, case_name, "count #{count}\n".b)
  end
end
