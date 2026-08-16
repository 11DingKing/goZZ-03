package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds runtime knobs. Defaults match the production rules (10-minute
// payment timeout, 30s reconciliation cadence, port 59041).
type Config struct {
	Port              string
	StorePath         string
	PaymentTimeout    time.Duration
	SchedulerInterval time.Duration
}

// Default returns the production defaults.
func Default() Config {
	return Config{
		Port:              "59041",
		StorePath:         "data/state.json",
		PaymentTimeout:    10 * time.Minute,
		SchedulerInterval: 30 * time.Second,
	}
}

// Load reads an optional JSON config file, then applies environment overrides.
// A missing file is not an error: defaults are used.
func Load(path string) (Config, error) {
	c := Default()
	if b, err := os.ReadFile(path); err == nil {
		var raw map[string]string
		if err := json.Unmarshal(b, &raw); err != nil {
			return c, fmt.Errorf("parse %s: %w", path, err)
		}
		applyRaw(&c, raw)
	} else if !os.IsNotExist(err) {
		return c, fmt.Errorf("read %s: %w", path, err)
	}
	applyEnv(&c)
	if err := c.validate(); err != nil {
		return c, err
	}
	return c, nil
}

func applyRaw(c *Config, raw map[string]string) {
	if v := raw["port"]; v != "" {
		c.Port = v
	}
	if v, ok := raw["store_path"]; ok {
		c.StorePath = v
	}
	if v := raw["payment_timeout"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.PaymentTimeout = d
		}
	}
	if v := raw["scheduler_interval"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.SchedulerInterval = d
		}
	}
}

func applyEnv(c *Config) {
	if v := os.Getenv("CEAE_PORT"); v != "" {
		c.Port = v
	}
	if v := os.Getenv("CEAE_STORE_PATH"); v != "" {
		c.StorePath = v
	}
	if v := os.Getenv("CEAE_PAYMENT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.PaymentTimeout = d
		}
	}
	if v := os.Getenv("CEAE_SCHEDULER_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.SchedulerInterval = d
		}
	}
}

func (c *Config) validate() error {
	if _, err := strconv.Atoi(c.Port); err != nil {
		return fmt.Errorf("invalid port %q: %w", c.Port, err)
	}
	if c.PaymentTimeout <= 0 {
		return fmt.Errorf("payment_timeout must be positive")
	}
	if c.SchedulerInterval <= 0 {
		return fmt.Errorf("scheduler_interval must be positive")
	}
	return nil
}
