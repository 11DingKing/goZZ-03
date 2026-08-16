package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"arcticdispatch/internal/config"
)

func TestConfigLoadFromFileAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"port":"6000","store_path":"data/x.json","payment_timeout":"5m","scheduler_interval":"15s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "6000" || cfg.PaymentTimeout != 5*time.Minute || cfg.SchedulerInterval != 15*time.Second || cfg.StorePath != "data/x.json" {
		t.Fatalf("bad config: %+v", cfg)
	}

	// A missing file falls back to production defaults.
	cfgDef, err := config.Load(filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfgDef.Port != "59041" || cfgDef.PaymentTimeout != 10*time.Minute {
		t.Fatalf("defaults wrong: %+v", cfgDef)
	}

	// An invalid port must error.
	if err := os.WriteFile(path, []byte(`{"port":"xx"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("want error for invalid port")
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"port":"6000","payment_timeout":"5m"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CEAE_PORT", "7000")
	t.Setenv("CEAE_PAYMENT_TIMEOUT", "2m")
	t.Setenv("CEAE_STORE_PATH", "data/env.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "7000" || cfg.PaymentTimeout != 2*time.Minute || cfg.StorePath != "data/env.json" {
		t.Fatalf("env override failed: %+v", cfg)
	}
}
