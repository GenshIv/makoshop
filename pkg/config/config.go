package config

import (
	"errors"
	"os"
	"strings"
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

	// Autocert enables automatic Let's Encrypt certificate issuance via ACME
	// (golang.org/x/crypto/acme/autocert). When AutocertDomains is non-empty,
	// the server obtains and renews certificates automatically instead of
	// using CertFile/KeyFile.
	AutocertDomains []string
	AutocertCache   string
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

// parseDomains splits a comma-separated domain list into a clean slice.
func parseDomains(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, d := range strings.Split(s, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			out = append(out, d)
		}
	}
	return out
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
				AutocertDomains: parseDomains(os.Getenv("MAKOSHOP_AUTOCERT_DOMAINS")),
				AutocertCache:   env("MAKOSHOP_AUTOCERT_CACHE", "certs"),
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

// AutocertEnabled reports whether automatic Let's Encrypt issuance is configured.
func (c Config) AutocertEnabled() bool {
	return len(c.Server.TLS.AutocertDomains) > 0
}

// TLSEnabled reports whether TLS is configured (either via explicit files or autocert).
func (c Config) TLSEnabled() bool {
	return c.AutocertEnabled() || (c.Server.TLS.CertFile != "" && c.Server.TLS.KeyFile != "")
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
	// Autocert and explicit files are mutually exclusive.
	if c.AutocertEnabled() && (c.Server.TLS.CertFile != "" || c.Server.TLS.KeyFile != "") {
		return errors.New("TLS misconfigured: choose either MAKOSHOP_AUTOCERT_DOMAINS or MAKOSHOP_TLS_CERT/KEY, not both")
	}
	return nil
}
