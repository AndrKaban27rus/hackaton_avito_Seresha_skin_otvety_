package config

import "os"

type Config struct{ Port, DatabaseURL, RedisAddr, KafkaBrokers, KafkaTopic string }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func Load() Config {
	return Config{
		Port:         env("PORT", "8080"),
		DatabaseURL:  env("DATABASE_URL", "postgres://buddy:buddy@localhost:5432/buddy?sslmode=disable"),
		RedisAddr:    env("REDIS_ADDR", "localhost:6379"),
		KafkaBrokers: env("KAFKA_BROKERS", "localhost:9092"),
		KafkaTopic:   env("KAFKA_TOPIC", "onboarding.events"),
	}
}
