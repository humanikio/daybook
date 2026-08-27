package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// Render writes the config people keep. A field that round-trips in memory but
// not to disk is a bug this file has produced before — enabled, days, fetch and
// authors each shipped that way, and the authors one silently dropped twenty
// commits from a report.
func TestPreviewSettingsSurviveRender(t *testing.T) {
	c := Default()
	c.Preview.Enabled = true
	c.Preview.Repos = []string{"frontend", "docs"}
	c.Preview.OnSchedule = true
	c.Output.Formats = []string{"html"}

	var got Config
	if err := yaml.Unmarshal(Render(c), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Preview.Repos) != 2 || got.Preview.Repos[0] != "frontend" {
		t.Errorf("preview.repos did not survive the write: %v", got.Preview.Repos)
	}
	if !got.Preview.OnSchedule {
		t.Error("preview.on_schedule did not survive the write")
	}
	if len(got.Output.Formats) != 1 || got.Output.Formats[0] != "html" {
		t.Errorf("output.formats did not survive the write: %v", got.Output.Formats)
	}
}

// An empty list must not be written as something that reads back as a choice.
func TestNoReposWritesNoList(t *testing.T) {
	c := Default()
	c.Preview.Enabled = true

	var got Config
	if err := yaml.Unmarshal(Render(c), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Preview.Repos) != 0 {
		t.Errorf("wrote a repo list nobody asked for: %v", got.Preview.Repos)
	}
	if !got.PreviewAllowsRepo("anything") {
		t.Error("a config with no list stopped allowing every repo")
	}
}
