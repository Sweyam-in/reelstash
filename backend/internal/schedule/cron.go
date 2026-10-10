// Package schedule runs recurring scans (cron expressions) and watches
// storage locations for file changes.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed 5-field cron expression: minute hour day-of-month month
// day-of-week. Each field supports *, lists (1,5), ranges (1-5) and steps
// (*/15, 10-50/10). Day-of-week is 0-6 (Sunday is 0; 7 is also accepted).
type Cron struct {
	minute, hour, dom, month, dow uint64
	domAny, dowAny                bool
}

var macros = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
}

// ParseCron parses a cron expression, or an @hourly/@daily/@weekly/@monthly macro.
func ParseCron(expr string) (Cron, error) {
	expr = strings.TrimSpace(expr)
	if m, ok := macros[strings.ToLower(expr)]; ok {
		expr = m
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Cron{}, fmt.Errorf("a cron expression has 5 fields (minute hour day month weekday), got %d", len(fields))
	}
	var c Cron
	var err error
	if c.minute, _, err = parseField(fields[0], 0, 59, "minute"); err != nil {
		return Cron{}, err
	}
	if c.hour, _, err = parseField(fields[1], 0, 23, "hour"); err != nil {
		return Cron{}, err
	}
	if c.dom, c.domAny, err = parseField(fields[2], 1, 31, "day of month"); err != nil {
		return Cron{}, err
	}
	if c.month, _, err = parseField(fields[3], 1, 12, "month"); err != nil {
		return Cron{}, err
	}
	if c.dow, c.dowAny, err = parseField(fields[4], 0, 7, "day of week"); err != nil {
		return Cron{}, err
	}
	if c.dow&(1<<7) != 0 { // 7 is Sunday too
		c.dow |= 1
	}
	return c, nil
}

// parseField returns the set of allowed values as a bitmask, and whether the
// field was an unrestricted "*" (day-of-month/week combine differently then).
func parseField(field string, min, max int, name string) (mask uint64, any bool, err error) {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, s, found := strings.Cut(part, "/"); found {
			step, err = strconv.Atoi(s)
			if err != nil || step < 1 {
				return 0, false, fmt.Errorf("invalid step %q in %s", s, name)
			}
			part = base
		}
		lo, hi := min, max
		switch {
		case part == "*":
			if step == 1 && !strings.Contains(field, ",") {
				any = true
			}
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			if lo, err = strconv.Atoi(a); err != nil {
				return 0, false, fmt.Errorf("invalid %s %q", name, part)
			}
			if hi, err = strconv.Atoi(b); err != nil {
				return 0, false, fmt.Errorf("invalid %s %q", name, part)
			}
		default:
			if lo, err = strconv.Atoi(part); err != nil {
				return 0, false, fmt.Errorf("invalid %s %q", name, part)
			}
			hi = lo
			if strings.Contains(field, "/") {
				hi = max // "5/10" means from 5 to the end, stepping
			}
		}
		if lo < min || hi > max || lo > hi {
			return 0, false, fmt.Errorf("%s must be between %d and %d", name, min, max)
		}
		for v := lo; v <= hi; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, any, nil
}

// Matches reports whether the expression fires during the minute containing t.
func (c Cron) Matches(t time.Time) bool {
	if c.minute&(1<<uint(t.Minute())) == 0 || c.hour&(1<<uint(t.Hour())) == 0 || c.month&(1<<uint(t.Month())) == 0 {
		return false
	}
	domOK := c.dom&(1<<uint(t.Day())) != 0
	dowOK := c.dow&(1<<uint(t.Weekday())) != 0
	// Classic cron: when both day fields are restricted, either may match.
	if !c.domAny && !c.dowAny {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// Next returns the first minute after t at which the expression fires, or
// the zero time if none occurs within five years (e.g. "0 0 31 2 *").
func (c Cron) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if c.Matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}
