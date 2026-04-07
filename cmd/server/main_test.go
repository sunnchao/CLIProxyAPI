package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverLegacyLocalPaths_UsesConfigAuthDir(t *testing.T) {
	wd := t.TempDir()
	home := filepath.Join(wd, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	cfgPath := filepath.Join(wd, "config.yaml")
	authDir := filepath.Join(wd, "custom-auth")
	if err := os.WriteFile(cfgPath, []byte("auth-dir: \""+authDir+"\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	gotConfig, gotAuth, err := discoverLegacyLocalPaths("", wd)
	if err != nil {
		t.Fatalf("discoverLegacyLocalPaths error: %v", err)
	}
	if gotConfig != cfgPath {
		t.Fatalf("expected config path %s, got %s", cfgPath, gotConfig)
	}
	if gotAuth != authDir {
		t.Fatalf("expected auth dir %s, got %s", authDir, gotAuth)
	}
}

func TestDiscoverLegacyLocalPaths_FallsBackToDefaultAuthDirWhenConfigMissing(t *testing.T) {
	wd := t.TempDir()
	home := filepath.Join(wd, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	gotConfig, gotAuth, err := discoverLegacyLocalPaths("", wd)
	if err != nil {
		t.Fatalf("discoverLegacyLocalPaths error: %v", err)
	}
	wantConfig := filepath.Join(wd, "config.yaml")
	wantAuth := filepath.Join(home, ".cli-proxy-api")
	if gotConfig != wantConfig {
		t.Fatalf("expected config path %s, got %s", wantConfig, gotConfig)
	}
	if gotAuth != wantAuth {
		t.Fatalf("expected auth dir %s, got %s", wantAuth, gotAuth)
	}
}
