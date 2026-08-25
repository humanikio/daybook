//go:build !darwin && !linux

package tui

func width() int { return 80 }
