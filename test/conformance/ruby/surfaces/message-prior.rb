# frozen_string_literal: true

# Read a prior writer's batch under its announcement (FORMAT.md
# `message-prior`). The answer is what the public read returned: the batch
# re-saved, or `<case>.refused` holding this leg's own reason, which the
# harness never compares (docs/rulings.json `refusal-verdict-only`).
def self.run(manifest, outdir)
  connections = {}
  RubyConformance.entries(manifest, 'prior-connection').each { |row| connections[row[1]] = row }
  RubyConformance.entries(manifest, 'prior-message').each do |row|
    case_name, connection, root, _, wire = row[1, 5]
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
    raw = RubyConformance.read_bytes(wire)
    begin
      values = pkg.load_messages(cls, vocab, raw, [raw.bytesize, 256].max)
    rescue pkg::Refused => e
      RubyConformance.write_bytes(outdir, "#{case_name}.refused", e.reason.to_s.b)
      next
    end
    RubyConformance.write_bytes(outdir, case_name, pkg.save_messages(cls, values))
  end
end
