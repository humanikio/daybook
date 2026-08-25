// Package source is the adapter seam.
//
// claude-code is the only adapter today. The interface exists so a second one
// (another agent CLI writing its own transcript format) can be added without
// touching derive/ or render/ — those depend on model.Stream and nothing else.
package source

import (
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// Window bounds a scan.
type Window struct {
	Start time.Time
	End   time.Time
	// Scope "window" reports only messages inside [Start,End]; "session"
	// reports the whole session that was active in it.
	Scope string
}

// Result is what an adapter returns.
type Result struct {
	Streams []model.Stream
	// ParseErrors counts unreadable lines. The transcript format is
	// undocumented and shifts between versions, so this is expected to be
	// non-zero one day; a silent zero would be the real failure.
	ParseErrors int
}

// Source reads agent transcripts.
type Source interface {
	Name() string
	Streams(cfg config.Config, w Window) (Result, error)
}
