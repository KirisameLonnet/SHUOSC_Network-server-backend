package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level server configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	WireGuard WireGuardConfig `yaml:"wireguard"`
	JWT       JWTConfig       `yaml:"jwt"`
	Invite    InviteConfig    `yaml:"invite"`
	Admin     AdminConfig     `yaml:"admin"`
	Discovery DiscoveryConfig `yaml:"discovery"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int `yaml:"port"` // default: 8080
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
	SSLMode  string `yaml:"sslmode"`
}

// DSN returns the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

// WireGuardConfig holds WireGuard interface settings.
type WireGuardConfig struct {
	Interface  string `yaml:"interface"`   // default: "wg0"
	ListenPort int    `yaml:"listen_port"` // default: 51820
	PrivateKey string `yaml:"private_key"` // server WG private key (base64)
	Subnet     string `yaml:"subnet"`      // default: "10.100.0.0/24"
}

// JWTConfig holds JWT signing settings.
type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpiryHours int    `yaml:"expiry_hours"` // default: 24
}

// InviteConfig holds invite code generation defaults.
type InviteConfig struct {
	DefaultMaxUses    int `yaml:"default_max_uses"`    // default: 1
	DefaultExpiryDays int `yaml:"default_expiry_days"` // default: 30
}

// AdminConfig holds admin bootstrap settings.
type AdminConfig struct {
	StudentID string `yaml:"student_id"` // default: "admin"
	Password  string `yaml:"password"`   // required for bootstrap
}

// DiscoveryConfig holds Cloudflare Pages discovery settings.
type DiscoveryConfig struct {
	Enabled         bool   `yaml:"enabled"`          // default: false
	DiscoveryURL    string `yaml:"discovery_url"`    // CF Pages Function URL, e.g. "https://scnet.pages.dev/api/server-info"
	DiscoverySecret string `yaml:"discovery_secret"` // shared secret for POST auth
	ApiAddr         string `yaml:"api_addr"`         // default: "localhost:8080" but overridden to Lucky address
	WgEndpoint      string `yaml:"wg_endpoint"`     // default: "localhost:51820" but overridden to Lucky address
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Host:    "localhost",
			Port:    5432,
			User:    "scnet",
			Name:    "scnet",
			SSLMode: "disable",
		},
		WireGuard: WireGuardConfig{
			Interface:  "wg0",
			ListenPort: 51820,
			Subnet:     "10.100.0.0/24",
		},
		JWT: JWTConfig{
			ExpiryHours: 24,
		},
		Invite: InviteConfig{
			DefaultMaxUses:    1,
			DefaultExpiryDays: 30,
		},
		Admin: AdminConfig{
			StudentID: "admin",
		},
		Discovery: DiscoveryConfig{
			Enabled:  false,
			ApiAddr:  "localhost:8080",
			WgEndpoint: "localhost:51820",
		},
	}
}

// envVarPattern matches ${VAR_NAME} style placeholders.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Load reads and parses a YAML config file, substituting environment variables.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Substitute ${VAR_NAME} with os.Getenv("VAR_NAME")
	content := envVarPattern.ReplaceAllStringFunc(string(data), func(match string) string {
		// match is like "${SCNET_DB_PASSWORD}"
		inner := match[2 : len(match)-1] // strip ${ and }
		return os.Getenv(inner)
	})

	cfg := DefaultConfig()

	decoder := yaml.NewDecoder(strings.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Validate required fields
	if cfg.Database.Password == "" {
		return nil, fmt.Errorf("database.password is required")
	}
	if cfg.WireGuard.PrivateKey == "" {
		return nil, fmt.Errorf("wireguard.private_key is required")
	}
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("jwt.secret is required")
	}
	if cfg.Admin.Password == "" {
		return nil, fmt.Errorf("admin.password is required (for bootstrap)")
	}

	return cfg, nil
}

// Parse reads YAML from a byte slice, substituting environment variables.
func Parse(data []byte) (*Config, error) {
	content := envVarPattern.ReplaceAllStringFunc(string(data), func(match string) string {
		inner := match[2 : len(match)-1]
		return os.Getenv(inner)
	})

	cfg := DefaultConfig()

	decoder := yaml.NewDecoder(strings.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}
