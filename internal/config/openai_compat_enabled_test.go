package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_OpenAICompatibilityEnabledDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configYAML := []byte(`
openai-compatibility:
  - name: "default-on"
    base-url: "https://enabled.example.com/v1"
    responses-enabled: true
  - name: "disabled"
    enabled: false
    base-url: "https://disabled.example.com/v1"
`)
	if err := os.WriteFile(configPath, configYAML, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}

	if len(cfg.OpenAICompatibility) != 2 {
		t.Fatalf("OpenAICompatibility len = %d, want 2", len(cfg.OpenAICompatibility))
	}
	if !cfg.OpenAICompatibility[0].IsEnabled() {
		t.Fatal("expected provider without enabled field to default to enabled")
	}
	if !cfg.OpenAICompatibility[0].IsResponsesEnabled() {
		t.Fatal("expected provider with responses-enabled=true to enable responses mode")
	}
	if cfg.OpenAICompatibility[1].IsEnabled() {
		t.Fatal("expected provider with enabled=false to be disabled")
	}
	if cfg.OpenAICompatibility[1].IsResponsesEnabled() {
		t.Fatal("expected provider without responses-enabled field to default to disabled")
	}
}
