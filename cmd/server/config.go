package main

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type config struct {
	HTTPAddr      string
	DatabaseURL   string
	ItemsCacheTTL time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		HTTPAddr:      envOr("HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ItemsCacheTTL: 5 * time.Minute,
	}
	if cfg.DatabaseURL == "" {
		return config{}, errors.New("DATABASE_URL is required")
	}

	if v := os.Getenv("ITEMS_CACHE_TTL"); v != "" {
		ttl, err := time.ParseDuration(v)
		if err != nil || ttl <= 0 {
			return config{}, fmt.Errorf("invalid ITEMS_CACHE_TTL %q", v)
		}
		cfg.ItemsCacheTTL = ttl
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
