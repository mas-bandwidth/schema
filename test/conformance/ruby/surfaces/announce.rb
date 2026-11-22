# frozen_string_literal: true

# THE ANNOUNCEMENT (SPEC-TABLES §3.3; README, the 2026-11-22 surfaces).
#
# `write`: the Ruby leg's announcement writer is the compiler, which bakes
# the unit's announcement into the constant ANNOUNCEMENT; the unit reads that
# constant into its own vocabulary as it loads. The answer is ANNOUNCEMENT,
# or `refused` when the unit's runtime refuses its own announcement as it
# loads (FL-38: Refused vocabulary_too_large past 4096 entries), so the unit
# can announce nothing.
# `strict` / `lenient`: a fresh MessageVocabulary, strict: true or left at its
# default, reads the connection's announcement file.
# `send`: SaveMessages of one body, with no announcement read or written.
def self.load_unit(unit)
  [RubyConformance.unit_package(unit), nil]
rescue StandardError => e
  raise unless e.class.name.to_s.end_with?('::Refused') && e.respond_to?(:reason) &&
               e.reason == 'vocabulary_too_large'

  [nil, e]
end

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'announce').each do |row|
    case_name, unit, root, mode, input = row[1, 5]
    pkg, refusal = load_unit(unit)
    if refusal
      raise "#{case_name}: the unit refused its own announcement on a #{mode} row" unless mode == 'write'

      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    if pkg.nil? || !pkg.respond_to?(:announce_read) || !pkg.const_defined?(:MessageVocabulary, false)
      RubyConformance.absent(outdir, case_name, 'message form is not emitted for this unit')
      next
    end
    out =
      case mode
      when 'write'
        pkg::ANNOUNCEMENT.b
      when 'strict', 'lenient'
        vocab = pkg::MessageVocabulary.new(strict: mode == 'strict')
        begin
          pkg.announce_read(vocab, RubyConformance.read_bytes(input))
          "read\n".b
        rescue pkg::Refused
          "refused\n".b
        end
      when 'send'
        cls = RubyConformance.root_class(unit, root)
        raise "#{case_name}: root #{root} is not generated" if cls.nil?

        value = RubyConformance.load_or_fresh(cls, input)
        begin
          pkg.save_messages(cls, [value]).b
        rescue pkg::InvalidValue
          "refused\n".b
        end
      else
        raise "#{case_name}: unknown announce mode #{mode}"
      end
    RubyConformance.write_bytes(outdir, case_name, out)
  end
end
