# frozen_string_literal: true

# Card conformance driver: one manifest, one surface, one output directory.

require 'fileutils'

gen_dir = File.expand_path(ENV['CARDS_RUBY_GENERATED'] || 'build/cards-ruby')
$LOAD_PATH.unshift(gen_dir) unless $LOAD_PATH.include?(gen_dir)

facade_path = File.expand_path(ENV['CARDS_RUBY_FACADE'] || 'tables/cards/ruby/facade.rb')
require facade_path

module CardsDriver
  SURFACES = %w[load save render parse bounds upgrade edit taskhash view].freeze

  def self.report_text(report)
    "Retained=#{report.retained}\nRetainLost=#{report.retain_lost}\n" \
      "Widened=#{report.widened}\nUnknown=#{report.unknown}\n" \
      "KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n"
  end

  def self.view_text
    rows = []
    Cards.unit_view.tables.sort_by(&:name).each do |table|
      table.type.fields.each do |fd|
        shape = if fd.array_kind == 3
                  'list'
                elsif fd.codec == 'string_blob'
                  'blob'
                else
                  'value'
                end
        bound = shape == 'value' && fd.type_name == 'string' && fd.size.positive? ? fd.size.to_s : '-'
        rows << "#{table.name} #{fd.name} #{fd.type_name} #{shape} #{bound}\n"
      end
    end
    rows.join
  end

  def self.decode_utf8!(raw)
    text = raw.dup.force_encoding(Encoding::UTF_8)
    raise CardsFacade::CardError, 'malformed' unless text.valid_encoding?

    text
  end

  def self.run_one(surface, row, outdir)
    name = row[1]
    path = row[2]
    stem = name.sub(/\.[^.\/]+\z/, '')
    if surface == 'view'
      File.binwrite(File.join(outdir, 'view.txt'), view_text.b)
      return
    end
    data = File.binread(path)
    begin
      case surface
      when 'load'
        record = CardsFacade.load(data)
        File.binwrite(File.join(outdir, "#{stem}.json"), CardsFacade.to_json(record).b)
        File.binwrite(File.join(outdir, "#{stem}.report"), report_text(record.report).b)
      when 'save', 'bounds'
        File.binwrite(File.join(outdir, "#{stem}.wire"), CardsFacade.from_json(decode_utf8!(data)).wire)
      when 'render'
        File.binwrite(File.join(outdir, "#{stem}.txt"), CardsFacade.render(CardsFacade.load(data)).b)
      when 'parse'
        File.binwrite(File.join(outdir, "#{stem}.wire"), CardsFacade.parse(decode_utf8!(data)).wire)
      when 'upgrade'
        wire, report = CardsFacade.upgrade(data)
        File.binwrite(File.join(outdir, "#{stem}.wire"), wire)
        File.binwrite(File.join(outdir, "#{stem}.report"), report_text(report).b)
      when 'edit'
        record = CardsFacade.load(data)
        raise CardsFacade::CardError, 'malformed' if row.length != 4 || !row[3].include?('=')

        key, value = row[3].split('=', 2)
        case key
        when 'paths'
          CardsFacade.set_paths(record, value.empty? ? [] : value.split(',', -1))
        when 'needs'
          CardsFacade.set_needs(record, value.empty? ? [] : value.split(',', -1))
        when 'tier'
          CardsFacade.set_tier(record, value)
        when 'priority'
          CardsFacade.set_priority(record, value)
        when 'column'
          CardsFacade.set_column(record, value)
        when 'holder'
          CardsFacade.set_holder(record, value)
        else
          raise CardsFacade::CardError, 'malformed'
        end
        File.binwrite(File.join(outdir, "#{stem}.wire"), record.wire)
      when 'taskhash'
        File.binwrite(File.join(outdir, "#{stem}.hex"), CardsFacade.task_hash(CardsFacade.load(data)).b)
      end
    rescue CardsFacade::CardError => e
      raise unless %w[bounds save parse upgrade edit].include?(surface)

      File.binwrite(File.join(outdir, "#{stem}.refused"), "#{e.reason}\n".b)
    end
  end

  def self.main(argv)
    return 2 if argv.length < 2

    manifest, surface = argv[0, 2]
    if surface == 'list'
      puts SURFACES.join("\n")
      return 0
    end
    return 2 if !SURFACES.include?(surface) || argv.length != 3

    outdir = argv[2]
    FileUtils.mkdir_p(outdir)
    File.foreach(manifest, encoding: 'UTF-8') do |line|
      trimmed = line.strip
      next if trimmed.empty? || trimmed.start_with?('#')

      row = trimmed.split
      run_one(surface, row, outdir) if row[0] == surface
    end
    0
  end
end

exit(CardsDriver.main(ARGV))
