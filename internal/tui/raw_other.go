//go:build !darwin && !linux

package tui

import (
	"errors"
	"os"
)

// No raw mode here. Callers fall back to a numbered menu, which needs nothing
// beyond a line of input and works in every terminal including Windows.
func makeRaw(*os.File) (func(), error) { return nil, errors.New("raw mode unsupported") }
