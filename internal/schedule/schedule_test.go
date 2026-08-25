package schedule

import (
	"testing"
	"time"

	"github.com/tndigitalmark/daybook/internal/config"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func cfg(atStr string, catchUp bool, days ...string) config.Config {
	c := config.Default()
	c.Schedule.At = atStr
	c.Schedule.CatchUp = catchUp
	c.Schedule.Days = days
	return c
}

// The case the whole design exists for: the machine was asleep at the scheduled
// time. A "is it 23:30 now" scheduler silently skips the day; a slot-based one
// notices on wake.
func TestCatchUpAfterSleep(t *testing.T) {
	c := cfg("23:30", true)
	served := at("2026-08-23 23:30")
	now := at("2026-08-25 09:00") // lid closed through Monday night

	slot, due := Due(c, served, now)
	if !due {
		t.Fatal("a missed slot should still be due with catch_up on")
	}
	if want := at("2026-08-24 23:30"); !slot.Equal(want) {
		t.Fatalf("slot = %v, want %v", slot, want)
	}
}

// With catch_up off, a long-missed slot is deliberately abandoned rather than
// producing a stale report hours late.
func TestNoCatchUpSkipsStaleSlot(t *testing.T) {
	c := cfg("23:30", false)
	if _, due := Due(c, at("2026-08-23 23:30"), at("2026-08-25 09:00")); due {
		t.Fatal("stale slot should be skipped with catch_up off")
	}
	// ...but a slot from minutes ago is still served.
	if _, due := Due(c, at("2026-08-23 23:30"), at("2026-08-24 23:35")); !due {
		t.Fatal("a fresh slot should be due even with catch_up off")
	}
}

func TestServedSlotIsNotRerun(t *testing.T) {
	c := cfg("23:30", true)
	slot := at("2026-08-24 23:30")
	if _, due := Due(c, slot, at("2026-08-24 23:45")); due {
		t.Fatal("a slot already served must not run again")
	}
}

func TestWeekdayFiltering(t *testing.T) {
	c := cfg("18:00", true, "mon", "wed", "fri")
	// Saturday 2026-08-22 19:00 — the most recent allowed slot is Friday.
	slot, ok := LastSlot(c, at("2026-08-22 19:00"))
	if !ok {
		t.Fatal("expected a slot")
	}
	if slot.Weekday() != time.Friday {
		t.Fatalf("slot fell on %v, want Friday", slot.Weekday())
	}
}

func TestSlotBeforeTodaysTimeWalksBack(t *testing.T) {
	c := cfg("23:30", true)
	// 09:00 — today's 23:30 has not arrived, so the last slot is yesterday's.
	slot, ok := LastSlot(c, at("2026-08-24 09:00"))
	if !ok {
		t.Fatal("expected a slot")
	}
	if want := at("2026-08-23 23:30"); !slot.Equal(want) {
		t.Fatalf("slot = %v, want %v", slot, want)
	}
}
