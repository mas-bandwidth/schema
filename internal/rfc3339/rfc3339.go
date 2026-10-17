package rfc3339

// tableRFC3339Parse reads exactly RFC 3339 §5.6's `date-time` production and
// answers its UTC nanosecond count, or false. The spelling is the standard's,
// with the section's own allowance that `T` and `Z` may be lower case
// (docs/SPEC-TABLES.md §2.11, §16.2):
//
//	date-time    = full-date "T" full-time
//	full-date    = 4DIGIT "-" 2DIGIT "-" 2DIGIT
//	full-time    = 2DIGIT ":" 2DIGIT ":" 2DIGIT ["." 1*DIGIT] time-offset
//	time-offset  = "Z" / ("+" / "-") 2DIGIT ":" 2DIGIT
//
// Month is 01-12, the day is valid for that month and year, the hour is 00-23,
// the minute is 00-59 and the second is 00-59: second 60 is a leap second this
// kind refuses (§2.11). A fraction of one or more digits after a dot truncates
// to nanoseconds; a comma is not a fraction separator. The offset's hours are
// 00-23 and its minutes 00-59. Nothing may precede or follow the production.
//
// The count is the seconds since the Unix epoch times one billion plus the
// fraction, so an instant outside int64's nanosecond range — the years 1678
// through 2262, Time.UnixNano's domain — names no count and is refused too.
// The parser reads only its argument and allocates nothing.
func tableRFC3339Parse(text []byte) (int64, bool) {
	// the shortest production is "2006-01-02T15:04:05Z"
	if len(text) < 20 {
		return 0, false
	}
	if text[4] != '-' || text[7] != '-' || text[13] != ':' || text[16] != ':' {
		return 0, false
	}
	// §5.6: "T" and "Z" may be lower case
	if text[10] != 'T' && text[10] != 't' {
		return 0, false
	}

	year := 0
	for i := 0; i < 4; i++ {
		d, ok := tableRFC3339Digit(text, i)
		if !ok {
			return 0, false
		}
		year = year*10 + d
	}
	month, ok := tableRFC3339Two(text, 5)
	if !ok {
		return 0, false
	}
	day, ok := tableRFC3339Two(text, 8)
	if !ok {
		return 0, false
	}
	hour, ok := tableRFC3339Two(text, 11)
	if !ok {
		return 0, false
	}
	minute, ok := tableRFC3339Two(text, 14)
	if !ok {
		return 0, false
	}
	second, ok := tableRFC3339Two(text, 17)
	if !ok {
		return 0, false
	}

	if month < 1 || month > 12 {
		return 0, false
	}
	if day < 1 || day > tableRFC3339MonthDays(year, month) {
		return 0, false
	}
	if hour > 23 || minute > 59 || second > 59 { // §2.11 refuses the leap second
		return 0, false
	}

	// time-secfrac = "." 1*DIGIT, truncated to nanoseconds
	index := 19
	var nanos int64
	if index < len(text) && text[index] == '.' {
		index++
		digits := 0
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			if digits < 9 {
				nanos = nanos*10 + int64(text[index]-'0')
			}
			digits++
			index++
		}
		if digits == 0 {
			return 0, false
		}
		for ; digits < 9; digits++ {
			nanos *= 10
		}
	}

	var offset int64
	if index >= len(text) {
		return 0, false
	}
	switch text[index] {
	case 'Z', 'z':
		index++
		if index != len(text) {
			return 0, false
		}
	case '+', '-':
		sign := int64(1)
		if text[index] == '-' {
			sign = -1
		}
		index++
		offsetHour, ok := tableRFC3339Two(text, index)
		if !ok {
			return 0, false
		}
		index += 2
		if index >= len(text) || text[index] != ':' {
			return 0, false
		}
		index++
		offsetMinute, ok := tableRFC3339Two(text, index)
		if !ok {
			return 0, false
		}
		index += 2
		if offsetHour > 23 || offsetMinute > 59 {
			return 0, false
		}
		if index != len(text) {
			return 0, false
		}
		offset = sign * int64(offsetHour*3600+offsetMinute*60)
	default:
		return 0, false
	}

	seconds := tableRFC3339Days(year, month, day)*86400 +
		int64(hour*3600+minute*60+second) - offset

	// Time.UnixNano's domain: the second before 1677-09-21T00:12:43 is
	// -9223372037 and its fraction must reach 145224192 nanoseconds; the
	// second of 2262-04-11T23:47:16 is 9223372036 and its fraction may not
	// pass 854775807.
	const (
		minSecond = -9223372037
		maxSecond = 9223372036
		minNanos  = 145224192
		maxNanos  = 854775807
	)
	if seconds < minSecond || seconds > maxSecond {
		return 0, false
	}
	if seconds == minSecond && nanos < minNanos {
		return 0, false
	}
	if seconds == maxSecond && nanos > maxNanos {
		return 0, false
	}
	return seconds*1000000000 + nanos, true
}

// tableRFC3339Digit is one ASCII decimal digit at index i.
func tableRFC3339Digit(text []byte, i int) (int, bool) {
	if i < 0 || i >= len(text) {
		return 0, false
	}
	c := text[i]
	if c < '0' || c > '9' {
		return 0, false
	}
	return int(c - '0'), true
}

// tableRFC3339Two is a two-digit field at index i.
func tableRFC3339Two(text []byte, i int) (int, bool) {
	high, ok := tableRFC3339Digit(text, i)
	if !ok {
		return 0, false
	}
	low, ok := tableRFC3339Digit(text, i+1)
	if !ok {
		return 0, false
	}
	return high*10 + low, true
}

// tableRFC3339Leap is the Gregorian rule RFC 3339's full-date inherits from
// ISO 8601: a year divisible by 4, except a century not divisible by 400.
func tableRFC3339Leap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// tableRFC3339MonthDays is the number of days in one month of one year.
func tableRFC3339MonthDays(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if tableRFC3339Leap(year) {
			return 29
		}
		return 28
	}
	return 0
}

// tableRFC3339Days is the number of days from 1970-01-01 to year-month-day,
// negative before the epoch. It is the civil-days algorithm, exact for the
// four-digit years RFC 3339 allows.
func tableRFC3339Days(year, month, day int) int64 {
	y := int64(year)
	m := int64(month)
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yearOfEra := y - era*400
	monthOfYear := m + 9
	if m > 2 {
		monthOfYear = m - 3
	}
	dayOfYear := (153*monthOfYear+2)/5 + int64(day) - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}
