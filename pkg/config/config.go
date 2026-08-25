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

	// TLS is enabled when both CertFile and KeyFile are set.
	TLS TLSConfig
}

type TLSConfig struct {
	CertFile string
	KeyFile  string

	// HTTPPort, when set together with TLS, runs a plain-HTTP listener that
	// redirects all traffic to HTTPS (port).
	HTTPPort string
}

type DatabaseConfig struct {
	Path               string
	NumShards          int
	MaxTotalSize       uint64 // bytes
	NumBucketsPerShard uint64
}

type AuthConfig struct {
	// JWTSecret is the secret key for JWT signing.
	// Can be set via MAKOSHOP_JWT_SECRET or PKG_CONFIG environment variable.
	JWTSecret string
}

// env returns the value of the given environment variable, or def if unset/empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func DefaultConfig() Config {
	jwtSecret := os.Getenv("MAKOSHOP_JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = os.Getenv("PKG_CONFIG")
	}

	return Config{
		Server: ServerConfig{
			Host: env("MAKOSHOP_HOST", "0.0.0.0"),
			Port: env("MAKOSHOP_PORT", "9090"),
			TLS: TLSConfig{
				CertFile: os.Getenv("MAKOSHOP_TLS_CERT"),
				KeyFile:  os.Getenv("MAKOSHOP_TLS_KEY"),
				HTTPPort: os.Getenv("MAKOSHOP_HTTP_PORT"),
			},
		},
		Database: DatabaseConfig{
			Path:               env("MAKOSHOP_DB_PATH", "makoshop_db"),
			NumShards:          16,
			MaxTotalSize:       40 * 1024 * 1024 * 1024, // 40 GB
			NumBucketsPerShard: 5_000_000,
		},
		Auth: AuthConfig{
			JWTSecret: jwtSecret,
		},
	}
}

// TLSEnabled reports whether TLS is configured.
func (c Config) TLSEnabled() bool {
	return c.Server.TLS.CertFile != "" && c.Server.TLS.KeyFile != ""
}

// Validate checks that required configuration values are set.
// The JWT secret must always be provided explicitly via environment variable.
func (c Config) Validate() error {
	if c.Auth.JWTSecret == "" {
		return errors.New("JWT secret is not set: set MAKOSHOP_JWT_SECRET or PKG_CONFIG environment variable")
	}
	// If only one of the TLS files is set, that is a misconfiguration.
	if (c.Server.TLS.CertFile == "") != (c.Server.TLS.KeyFile == "") {
		return errors.New("TLS misconfigured: both MAKOSHOP_TLS_CERT and MAKOSHOP_TLS_KEY must be set (or neither)")
	}
	return nil
}
