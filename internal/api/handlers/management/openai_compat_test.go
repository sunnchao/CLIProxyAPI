package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestGetOpenAICompat_NormalizesResponsesEnabledDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "compat",
				BaseURL: "https://compat.example.com/v1",
			},
		},
	}, nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/openai-compatibility", nil)

	h.GetOpenAICompat(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var body struct {
		Items []struct {
			Name             string `json:"name"`
			ResponsesEnabled bool   `json:"responses-enabled"`
		} `json:"openai-compatibility"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(body.Items))
	}
	if body.Items[0].ResponsesEnabled {
		t.Fatal("expected responses-enabled to default to false in normalized response")
	}
}

func TestPatchOpenAICompat_UpdatesResponsesEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("openai-compatibility: []\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	h := NewHandler(&config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "compat",
				BaseURL: "https://compat.example.com/v1",
			},
		},
	}, configPath, nil)

	payload := []byte(`{"index":0,"value":{"responses-enabled":true}}`)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/openai-compatibility", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PatchOpenAICompat(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if len(h.cfg.OpenAICompatibility) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(h.cfg.OpenAICompatibility))
	}
	if !h.cfg.OpenAICompatibility[0].IsResponsesEnabled() {
		t.Fatal("expected responses-enabled to be updated in memory")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !bytes.Contains(data, []byte("responses-enabled: true")) {
		t.Fatalf("expected persisted config to include responses-enabled, got %s", string(data))
	}
}
