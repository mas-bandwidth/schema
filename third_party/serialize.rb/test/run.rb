# frozen_string_literal: true

# The test runner: every test/test_*.rb, under the interpreter that runs it.
#
#   usage: <ruby> -w test/run.rb [Class | Class#test_name ...]
#
# The library and the tests are found only where this file puts them: lib/ and
# test/ of this repository. A run that selects no test fails, by name, rather
# than passing over nothing, and so does a run in which Ruby printed a warning:
# the library and its tests are silent under ruby -w.

HERE = __dir__
ROOT = File.dirname(HERE)
$LOAD_PATH.unshift(File.join(ROOT, 'lib'), HERE)

# every warning Ruby emits, so a run that printed one is red
module WarningTrap
  CAUGHT = [] # rubocop:disable Style/MutableConstant -- the run's warnings

  def warn(message, *, **)
    CAUGHT << message
    super
  end
end
Warning.singleton_class.prepend(WarningTrap)

require 'harness'
Dir.glob(File.join(HERE, 'test_*.rb')).each { |path| require File.basename(path, '.rb') }

def selected(filters)
  all = Harness::CASES.flat_map { |k| k.tests.map { |t| [k, t] } }
  return all if filters.empty?

  all.select { |k, t| filters.include?(k.name) || filters.include?("#{k.name}##{t}") }
end

def main(filters)
  tests = selected(filters)
  if tests.empty?
    warn 'run.rb REFUSES: no test was selected'
    return 1
  end
  yjit = defined?(RubyVM::YJIT) && RubyVM::YJIT.enabled? ? 'on' : 'off'
  puts "serialize.rb tests under Ruby #{RUBY_VERSION} (YJIT #{yjit})"
  failed = 0
  tests.each do |klass, name|
    test = klass.new
    test.subtest(name) { test.public_send(name) }
    next if test.failures.empty?

    failed += 1
    puts "FAIL #{klass.name}##{name}"
    test.failures.each { |f| puts "  #{f}" }
  end
  unless WarningTrap::CAUGHT.empty?
    puts "FAIL Ruby printed #{WarningTrap::CAUGHT.size} warning(s) under -w:"
    WarningTrap::CAUGHT.each { |w| puts "  #{w}" }
    failed += 1
  end
  puts "#{tests.size} tests, #{failed} failed"
  failed.zero? ? 0 : 1
end

exit(main(ARGV))
