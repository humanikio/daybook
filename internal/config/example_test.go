package config

import (
	"os"
	"testing"
)

// config.example.yaml is what docs/setup.md sends people to. A key it does not
// mention is a feature nobody finds, and a key it spells wrong is worse.
func TestExampleConfigLoads(t *testing.T) {
	b, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Skip(err)
	}
	tmp := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(tmp)
	if err != nil {
		t.Fatalf("the annotated example does not load: %v", err)
	}
	if c.Preview.MaxPhotos == 0 {
		t.Error("example has no preview block")
	}
	if c.Preview.Enabled || c.Preview.OnSchedule || c.Preview.StartServers {
		t.Error("the example must ship with the browser-touching switches off")
	}
}
