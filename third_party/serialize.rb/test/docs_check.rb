# frozen_string_literal: true

# The documentation check: every ruby code block of the named Markdown files
# runs against lib/ under ruby -w, and every relative link names a file that
# exists. Offline and standard library only.
#
#   usage: <ruby> -w test/docs_check.rb [--root DIR] FILE...
#          <ruby> -w test/docs_check.rb --negative-control FILE
#
# Each ```ruby block runs on its own, in a fresh interpreter (the one running
# this file), from an empty scratch directory, with lib/ on the load path and
# no RUBYOPT or RUBYLIB of the caller's. A block that raises, exits non-zero or
# prints anything on stderr (a warning under -w) fails, named by its file and
# line. A link [text](target) outside code, whose target is not a URL or an
# anchor, must name a file or directory under --root (default: the directory
# of the file that holds it). A run that finds no ruby block fails: a check
# that ran nothing is never green.
#
# --negative-control copies FILE into build/docs-negative-control/ and plants,
# in one copy each, a block that raises, a block that warns and a broken
# relative link; the unplanted copy must pass, and each planted copy must fail
# by the name of what was planted.

require 'open3'
require 'rbconfig'
require 'tmpdir'
require 'fileutils'

# The check and its negative control.
module DocsCheck
  ROOT = File.dirname(__dir__)
  LIB = File.join(ROOT, 'lib')
  FENCE = /\A(`{3,})\s*([\w+-]*)\s*\z/
  LINK = /(?<!!)\[[^\]]*\]\(([^()\s]+)(?:\s+"[^"]*")?\)/
  URL = /\A[a-z][a-z0-9+.-]*:/i

  module_function

  # [[line, language, code]] for every fenced block, and [[line, text]] for
  # every line outside one, inline code spans removed
  def parse(text)
    blocks = []
    prose = []
    open = nil
    text.each_line.with_index(1) do |line, number|
      fence = FENCE.match(line.chomp)
      if open
        if fence && fence[1].size >= open[:ticks] && fence[2].empty?
          blocks << [open[:line], open[:language], open[:code]]
          open = nil
        else
          open[:code] << line
        end
      elsif fence
        open = { ticks: fence[1].size, line: number, language: fence[2], code: +'' }
      else
        prose << [number, line.gsub(/`[^`]*`/, '')]
      end
    end
    blocks << [open[:line], open[:language], open[:code]] if open
    [blocks, prose]
  end

  # nil when the block ran clean, or why it did not
  def run_block(code)
    scratch = File.join(ROOT, 'build')
    FileUtils.mkdir_p(scratch)
    Dir.mktmpdir('docs-check', scratch) do |dir|
      path = File.join(dir, 'block.rb')
      File.write(path, code)
      env = { 'RUBYOPT' => nil, 'RUBYLIB' => nil }
      _out, err, status = Open3.capture3(env, RbConfig.ruby, '-w', '-I', LIB, path, chdir: dir)
      err = err.dup.force_encoding(Encoding::UTF_8).scrub
      first = err.lines.first.to_s.strip.sub(path, 'block')
      return "the ruby block exited #{status.exitstatus}: #{first}" unless status.success?
      return "the ruby block printed on stderr (a warning under -w): #{first}" unless err.empty?

      nil
    end
  end

  # every failure of one file, each "file:line: why"; counts go into tally
  def check_file(file, root, tally)
    failures = []
    blocks, prose = parse(File.read(file, encoding: Encoding::UTF_8))
    blocks.each do |line, language, code|
      next unless language == 'ruby'

      tally[:blocks] += 1
      why = run_block(code)
      failures << "#{file}:#{line}: #{why}" if why
    end
    base = root || File.dirname(file)
    prose.each do |line, text|
      text.scan(LINK) do |(target)|
        next if target.start_with?('#') || URL.match?(target)

        tally[:links] += 1
        path = target.split('#', 2).first
        next if File.exist?(File.join(base, path))

        failures << "#{file}:#{line}: the relative link #{target} names no file"
      end
    end
    failures
  end

  def check(files, root)
    tally = { blocks: 0, links: 0 }
    failures = files.flat_map { |file| check_file(file, root, tally) }
    failures << "no ruby block in #{files.join(' ')}: a check that ran nothing is never green" if tally[:blocks].zero?
    failures.each { |f| puts "docs-check FAILED: #{f}" }
    return 1 unless failures.empty?

    puts "docs-check: #{tally[:blocks]} ruby blocks ran clean under ruby -w and " \
         "#{tally[:links]} relative links resolve in #{files.join(', ')}"
    0
  end

  # [name, text appended to the copy, the failure line it must produce]
  PLANTS = [
    ['raises', "\n```ruby\nraise 'planted: a broken example'\n```\n",
     'the ruby block exited 1: .*planted: a broken example'],
    ['warns', "\n```ruby\nplanted = 1\n```\n", 'the ruby block printed on stderr .*unused variable - planted'],
    ['link', "\n[planted](no-such-file-planted.md)\n", 'the relative link no-such-file-planted.md names no file']
  ].freeze

  def negative_control(file)
    dir = File.join(ROOT, 'build', 'docs-negative-control')
    FileUtils.rm_rf(dir)
    FileUtils.mkdir_p(dir)
    original = File.read(file, encoding: Encoding::UTF_8)
    clean = File.join(dir, File.basename(file))
    File.write(clean, original)
    out, status = docs_check(clean)
    unless status.success?
      puts out
      puts "NEGATIVE CONTROL FAILED: the unplanted copy of #{file} did not pass"
      return 1
    end
    PLANTS.each do |name, text, want|
      copy = File.join(dir, "#{name}-#{File.basename(file)}")
      File.write(copy, original + text)
      line = original.lines.size + 2
      out, status = docs_check(copy)
      pattern = /^docs-check FAILED: #{Regexp.escape(copy)}:#{line}: #{want}/
      next if !status.success? && pattern.match?(out)

      puts out
      puts "NEGATIVE CONTROL FAILED: the copy planted with a block that #{name} " \
           "#{status.success? ? 'passed' : 'went red, but not by name'}"
      return 1
    end
    puts 'docs-check negative control: a block that raises, a block that warns and a broken link, ' \
         "each planted in a copy of #{file}, turn docs-check red by name"
    0
  end

  # docs-check over one copy, links resolved against the repository root
  def docs_check(copy)
    env = { 'RUBYOPT' => nil, 'RUBYLIB' => nil }
    out, status = Open3.capture2e(env, RbConfig.ruby, '-w', __FILE__, '--root', ROOT, copy)
    [out.force_encoding(Encoding::UTF_8).scrub, status]
  end

  def main(args)
    if args.first == '--negative-control'
      return negative_control(args[1]) if args.size == 2

      warn 'docs_check.rb REFUSES: --negative-control takes one file'
      return 2
    end
    root = nil
    root = args.slice!(0, 2).last if args.first == '--root'
    if args.empty?
      warn 'docs_check.rb REFUSES: name the Markdown files to check'
      return 2
    end
    missing = args.reject { |f| File.file?(f) }
    unless missing.empty?
      warn "docs_check.rb REFUSES: no such file: #{missing.join(' ')}"
      return 2
    end
    check(args, root)
  end
end

exit(DocsCheck.main(ARGV.dup)) if $PROGRAM_NAME == __FILE__
