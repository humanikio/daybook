// Package tui is the small amount of interactivity daybook needs: a list you
// move through with the arrow keys.
//
// It degrades rather than requiring anything. Without a terminal — a pipe, a
// CI job, Windows — the same call renders a numbered list and reads one line.
// Every path through this package works with nothing but stdin and stdout.
package tui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Item is one row.
type Item struct {
	Label string // what the setting is called
	Value string // what it is set to now
	Hint  string // one line under it when selected
}

const (
	esc   = "\033["
	clear = esc + "2K"
	up    = esc + "A"
	hide  = esc + "?25l"
	show  = esc + "?25h"
)

// Select renders the list and returns the chosen index, or -1 to quit.
func Select(title string, items []Item, colour bool) int {
	if len(items) == 0 {
		return -1
	}
	if restore, err := makeRaw(os.Stdin); err == nil {
		defer restore()
		return selectRaw(title, items, colour)
	}
	return selectNumbered(title, items)
}

func selectRaw(title string, items []Item, colour bool) int {
	dim, bold, cyan, reset := "", "", "", ""
	if colour {
		dim, bold, cyan, reset = "\033[2m", "\033[1m", "\033[36m", "\033[0m"
	}

	fmt.Print(hide)
	defer fmt.Print(show)

	cur := 0
	draw := func(first bool) {
		if !first {
			// Walk back over everything drawn last time. Redrawing in place
			// keeps the chooser to one screen region instead of scrolling a
			// new copy of the list on every keypress.
			for i := 0; i < len(items)+2; i++ {
				fmt.Print(up + clear + "\r")
			}
		}
		fmt.Printf("%s%s%s\r\n", bold, title, reset)
		fmt.Printf("%s  ↑↓ move · enter edit · q done%s\r\n", dim, reset)
		for i, it := range items {
			mark, style := "  ", ""
			if i == cur {
				mark, style = cyan+"▸ "+reset, bold
			}
			fmt.Printf("%s%s%-18s%s %s\r\n", mark, style, it.Label, reset, it.Value)
		}
	}
	draw(true)

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return -1
		}
		switch {
		case buf[0] == 'q' || buf[0] == 3 || buf[0] == 4: // q, ctrl-c, ctrl-d
			return -1
		case buf[0] == '\r' || buf[0] == '\n':
			return cur
		case buf[0] == 'k':
			cur = (cur - 1 + len(items)) % len(items)
		case buf[0] == 'j':
			cur = (cur + 1) % len(items)
		case n == 3 && buf[0] == 27 && buf[1] == '[':
			switch buf[2] {
			case 'A':
				cur = (cur - 1 + len(items)) % len(items)
			case 'B':
				cur = (cur + 1) % len(items)
			}
		case buf[0] == 27 && n == 1: // bare escape
			return -1
		}
		draw(false)
	}
}

// selectNumbered is the everywhere-else path: no raw mode, no escape codes.
func selectNumbered(title string, items []Item) int {
	fmt.Println(title)
	for i, it := range items {
		fmt.Printf("  %d  %-18s %s\n", i+1, it.Label, it.Value)
	}
	fmt.Print("  number to edit, or blank to finish: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(items) {
		return -1
	}
	return n - 1
}

// Prompt asks for one value, with the current one as the default.
//
// Deliberately NOT in raw mode: this needs line editing, backspace and paste,
// all of which the terminal already does when it is left alone.
func Prompt(label, current string) string {
	fmt.Printf("  %s [%s]: ", label, current)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return current
	}
	if s := strings.TrimSpace(line); s != "" {
		return s
	}
	return current
}

// Interactive reports whether stdin is a terminal a person is watching.
func Interactive() bool { return IsTerminal(os.Stdin) }
