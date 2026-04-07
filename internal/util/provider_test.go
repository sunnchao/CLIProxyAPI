package util

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestOpenAICompatibilityHelpersSkipDisabledProviders(t *testing.T) {
	disabled := false
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "enabled-provider",
				BaseURL: "https://enabled.example.com/v1",
				Models: []config.OpenAICompatibilityModel{
					{Name: "upstream-a", Alias: "shared-alias"},
				},
			},
			{
				Name:    "disabled-provider",
				Enabled: &disabled,
				BaseURL: "https://disabled.example.com/v1",
				Models: []config.OpenAICompatibilityModel{
					{Name: "upstream-b", Alias: "disabled-alias"},
				},
			},
		},
	}

	if !IsOpenAICompatibilityAlias("shared-alias", cfg) {
		t.Fatal("expected enabled alias to be found")
	}
	if IsOpenAICompatibilityAlias("disabled-alias", cfg) {
		t.Fatal("expected disabled alias to be ignored")
	}

	compat, model := GetOpenAICompatibilityConfig("disabled-alias", cfg)
	if compat != nil || model != nil {
		t.Fatal("expected disabled provider to be ignored by config lookup")
	}

	compat, model = GetOpenAICompatibilityConfig("shared-alias", cfg)
	if compat == nil || model == nil || compat.Name != "enabled-provider" || model.Name != "upstream-a" {
		t.Fatalf("unexpected enabled provider lookup result: compat=%v model=%v", compat, model)
	}
}
