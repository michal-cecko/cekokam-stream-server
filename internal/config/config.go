package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DashboardURL   string
	DashboardToken string
	PublicURL      string
	StorageDir     string
	ListenAddr     string
	PollInterval   time.Duration
	PruneInterval  time.Duration
	PreserveCount  int
	SegmentTimeout time.Duration
	SyncInterval   time.Duration
}

func Load() (Config, error) {
	c := Config{
		ListenAddr:     env("LISTEN_ADDR", ":8080"),
		PollInterval:   envDuration("POLL_INTERVAL", 12*time.Second),
		PruneInterval:  envDuration("PRUNE_INTERVAL", 3*time.Minute),
		PreserveCount:  envInt("PRESERVE_COUNT", 50),
		SegmentTimeout: envDuration("SEGMENT_TIMEOUT", 30*time.Second),
		SyncInterval:   60 * time.Second,
	}

	c.DashboardURL = strings.TrimRight(os.Getenv("DASHBOARD_URL"), "/")
	c.DashboardToken = os.Getenv("DASHBOARD_TOKEN")
	c.PublicURL = strings.TrimRight(os.Getenv("PUBLIC_URL"), "/")
	c.StorageDir = os.Getenv("STORAGE_DIR")

	var missing []string
	if c.DashboardURL == "" {
		missing = append(missing, "DASHBOARD_URL")
	}
	if c.DashboardToken == "" {
		missing = append(missing, "DASHBOARD_TOKEN")
	}
	if c.PublicURL == "" {
		missing = append(missing, "PUBLIC_URL")
	}
	if c.StorageDir == "" {
		missing = append(missing, "STORAGE_DIR")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if c.PreserveCount < 1 {
		return Config{}, errors.New("PRESERVE_COUNT must be >= 1")
	}

	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
