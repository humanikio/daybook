// Package schedule decides when the next run is due.
//
// The whole design turns on one idea: a run is owed to a SLOT, not to a moment.
// A slot is a scheduled time that has passed — "23:30 on Monday". Asking "is it
// 23:30 right now" fails the moment the machine is asleep at 23:30, which for a
// laptop is the normal case rather than the edge. Asking "is there a slot I have
// not served yet" gets catch-up for free and needs no wake-up notifications from
// the OS.
package schedule

import (
	"strings"
	"time"

	"github.com/tndigitalmark/claude-code-daybook/internal/config"
)

// weekdays maps the config's day names. Long forms and three-letter forms both
// work, because people write both and rejecting one is pointless pedantry.
var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

// Allowed reports whether the schedule runs on this weekday. Empty means daily.
func Allowed(cfg config.Config, d time.Weekday) bool {
	if len(cfg.Schedule.Days) == 0 {
		return true
	}
	for _, s := range cfg.Schedule.Days {
		if w, ok := weekdays[strings.ToLower(strings.TrimSpace(s))]; ok && w == d {
			return true
		}
	}
	return false
}

// LastSlot returns the most recent scheduled time at or before now, and whether
// one exists.
//
// It walks back at most eight days: seven covers any weekday selection, and the
// eighth absorbs the case where today's slot has not arrived yet.
func LastSlot(cfg config.Config, now time.Time) (time.Time, bool) {
	at := cfg.Schedule.At
	if at == "" {
		at = "23:30"
	}
	hm, err := time.Parse("15:04", at)
	if err != nil {
		return time.Time{}, false
	}
	for i := 0; i < 8; i++ {
		d := now.AddDate(0, 0, -i)
		slot := time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
		if slot.After(now) {
			continue // today's slot has not arrived
		}
		if !Allowed(cfg, slot.Weekday()) {
			continue
		}
		return slot, true
	}
	return time.Time{}, false
}

// Due reports whether a run is owed, given the last slot that was served.
//
// catch_up is what separates the two behaviours, and it is the difference
// between a scheduler that works on a laptop and one that only works on a
// server:
//
//	true  — any unserved slot is due, however long ago. Close the lid at 23:00
//	        and open it at 09:00 and you get yesterday's report on wake.
//	false — only a slot from the last hour is due. Miss it and the day is
//	        skipped, which is what you want on a machine that is always on and
//	        where a stale report would be worse than none.
func Due(cfg config.Config, lastServed time.Time, now time.Time) (time.Time, bool) {
	slot, ok := LastSlot(cfg, now)
	if !ok {
		return time.Time{}, false
	}
	if !lastServed.Before(slot) {
		return time.Time{}, false // already served this slot
	}
	if !cfg.Schedule.CatchUp && now.Sub(slot) > time.Hour {
		return time.Time{}, false
	}
	return slot, true
}

// Next returns the next scheduled time strictly after now, for display.
func Next(cfg config.Config, now time.Time) (time.Time, bool) {
	at := cfg.Schedule.At
	if at == "" {
		at = "23:30"
	}
	hm, err := time.Parse("15:04", at)
	if err != nil {
		return time.Time{}, false
	}
	for i := 0; i < 8; i++ {
		d := now.AddDate(0, 0, i)
		slot := time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
		if !slot.After(now) || !Allowed(cfg, slot.Weekday()) {
			continue
		}
		return slot, true
	}
	return time.Time{}, false
}
