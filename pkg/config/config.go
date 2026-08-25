package config

import (
	"errors"
	"os"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Auth     AuthConfig
}

type ServerConfig struct {
	Host string
	Port string
}

type DatabaseConfig struct {
	Path               string
	NumShards          int
	MaxTotalSize       uint64 // bytes
	NumBucketsPerShard uint64
}

type AuthConfig struct {
	// JWTSecret is the secret key for JWT signing.
	// Can be set via PKG_CONFIG or MAKOSHOP_JWT_SECRET environment variable.
	JWTSecret string
}

func DefaultConfig() Config {
	jwtSecret := os.Getenv("MAKOSHOP_JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = os.Getenv("PKG_CONFIG")
	}

	return Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: "9090",
		},
		Database: DatabaseConfig{
			Path:               "makoshop_db",
			NumShards:          16,
			MaxTotalSize:       40 * 1024 * 1024 * 1024, // 50 GB
			NumBucketsPerShard: 5_000_000,
		},
		Auth: AuthConfig{
			JWTSecret: jwtSecret,
		},
	}
}

// Validate checks that required configuration values are set.
// The JWT secret must always be provided explicitly via environment variable.
func (c Config) Validate() error {
	if c.Auth.JWTSecret == "" {
		return errors.New("JWT secret is not set: set MAKOSHOP_JWT_SECRET or PKG_CONFIG environment variable")
	}
	return nil
}
