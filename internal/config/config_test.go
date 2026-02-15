package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joebot/nagobot/internal/config"
)

func TestSavePreservesEnvKeys(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Save to temp file
	tmp := filepath.Join(t.TempDir(), "config.json")
	if err := config.SaveTo(cfg, tmp); err != nil {
		t.Fatal(err)
	}

	// Reload from saved file
	saved, err := config.LoadFrom(tmp)
	if err != nil {
		t.Fatal(err)
	}

	srv := saved.MCP.Servers["amap-mcp-server"]
	t.Logf("saved env: %v", srv.Env)
	if _, ok := srv.Env["AMAP_MAPS_API_KEY"]; !ok {
		t.Errorf("AMAP_MAPS_API_KEY lost after save, got keys: %v", srv.Env)
	}
}

func TestMCPEnvKeyPreserved(t *testing.T) {
	cfg, _ := config.Load()
	srv := cfg.MCP.Servers["amap-mcp-server"]
	t.Logf("env: %v", srv.Env)
	if _, ok := srv.Env["AMAP_MAPS_API_KEY"]; !ok {
		t.Errorf("AMAP_MAPS_API_KEY not found, got keys: %v", srv.Env)
	}
}

func TestValidateRejectsInvalid(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(tmp, []byte(`{
		"agents":{"defaults":{"maxToolIterations":-5,"temperature":3.5}},
		"channels":{"discord":{"enabled":true,"token":""}},
		"unknownField": true
	}`), 0o644)

	_, err := config.LoadFrom(tmp)
	if err == nil {
		t.Fatal("expected validation error")
	}
	t.Log(err)
}
