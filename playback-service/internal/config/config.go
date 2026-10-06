package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Port           string
	RedisAddr      string
	KafkaBrokers   []string
	KafkaTopic     string
	CatalogURL     string
	InternalToken  string
	AccessCacheTTL time.Duration
}

func Load() Config {
	return Config{
		Port:           getEnv("PORT", "8081"),
		RedisAddr:      getEnv("REDIS_ADDR", "localhost:6379"),
		KafkaBrokers:   strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ","),
		KafkaTopic:     getEnv("KAFKA_EPISODE_COMPLETED_TOPIC", "playback.episode_completed"),
		CatalogURL:     getEnv("CATALOG_URL", "http://localhost:8000"),
		InternalToken:  getEnv("INTERNAL_API_TOKEN", "dev-internal-token"),
		AccessCacheTTL: 5 * time.Minute,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
