package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser reads the five standard fields (minute, hour, day of the
// month, month and weekday) and descriptors such as "@daily".
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// recurrence gives the occurrences of a schedule.
type recurrence interface {
	// next returns the first occurrence strictly after after, for a schedule
	// armed at armed.
	next(armed, after time.Time) time.Time
}

// Next returns the schedule's first occurrence strictly after after, for a
// schedule armed at armed (an interval counts from there; a cron expression
// ignores it). Times are read in the machine's time zone. It returns the zero
// time for an invalid schedule.
func (s Schedule) Next(armed, after time.Time) time.Time {
	return s.nextIn(time.Local, armed, after)
}

// nextIn is Next in a given time zone, for tests.
func (s Schedule) nextIn(loc *time.Location, armed, after time.Time) time.Time {
	r, err := s.recurrence(loc)
	if err != nil {
		return time.Time{}
	}

	return r.next(armed, after)
}

// recurrence parses Every or Cron; exactly one must be set.
func (s Schedule) recurrence(loc *time.Location) (recurrence, error) {
	every, spec := strings.TrimSpace(s.Every), strings.TrimSpace(s.Cron)

	switch {
	case every != "" && spec != "":
		return nil, errors.New("give either every or cron, not both")
	case every != "":
		return parseInterval(every)
	case spec != "":
		return parseCron(spec, loc)
	default:
		return nil, errors.New("a schedule needs every (e.g. 10m) or cron (e.g. \"0 9 * * 1-5\")")
	}
}

// interval is "every N", counted from when the schedule was armed.
type interval time.Duration

func parseInterval(s string) (interval, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("every %q: a duration such as 10m, 2h or 1h30m", s)
	}

	if d < MinInterval {
		return 0, fmt.Errorf("every %q: at least %s", s, MinInterval)
	}

	return interval(d), nil
}

func (i interval) next(armed, after time.Time) time.Time {
	d := time.Duration(i)
	if after.Before(armed) {
		return armed.Add(d)
	}

	// The occurrences are armed + k·d: the first one after "after" is what
	// is left of the current period.
	return after.Add(d - after.Sub(armed)%d)
}

// cronSchedule is a cron expression in a time zone. Around daylight saving
// changes it behaves as robfig/cron does (pinned by recurrence tests).
type cronSchedule struct {
	schedule cron.Schedule
}

func parseCron(spec string, loc *time.Location) (cronSchedule, error) {
	// Times are read in the machine's time zone (ADR-0012): no per-schedule
	// zone, and "@every" would duplicate the every field.
	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
		return cronSchedule{}, fmt.Errorf("cron %q: time zones are not supported, the machine's own is used", spec)
	}

	if strings.HasPrefix(spec, "@every") {
		return cronSchedule{}, fmt.Errorf("cron %q: use the every field for an interval", spec)
	}

	parsed, err := cronParser.Parse(spec)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("cron %q: %w", spec, err)
	}

	if s, ok := parsed.(*cron.SpecSchedule); ok {
		s.Location = loc
	}

	return cronSchedule{schedule: parsed}, nil
}

func (c cronSchedule) next(_, after time.Time) time.Time {
	return c.schedule.Next(after)
}
