package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level server configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	WireGuard WireGuardConfig `yaml:"wireguard"`
	Punch     PunchConfig     `yaml:"punch"`
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
	Interface  string `yaml:"interface"`   // default: "wg_scnet"
	ListenPort int    `yaml:"listen_port"` // default: 51820
	PrivateKey string `yaml:"private_key"` // server WG private key (base64)
	Subnet     string `yaml:"subnet"`      // default: "10.100.0.0/24"
}

// PunchConfig holds optional UDP punch/proxy settings.
type PunchConfig struct {
	Enabled                  bool     `yaml:"enabled"`
	ListenPort               int      `yaml:"listen_port"`
	WireGuardHost            string   `yaml:"wireguard_host"`
	WireGuardPort            int      `yaml:"wireguard_port"`
	STUNServers              []string `yaml:"stun_servers"`
	ProbeTimeoutSeconds      int      `yaml:"probe_timeout_seconds"`
	KeepaliveIntervalSeconds int      `yaml:"keepalive_interval_seconds"`
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
	WgEndpoint      string `yaml:"wg_endpoint"`      // default: "localhost:51820" but overridden to Lucky address
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
			Interface:  "wg_scnet",
			ListenPort: 51820,
			Subnet:     "10.100.0.0/24",
		},
		Punch: PunchConfig{
			Enabled:                  false,
			ListenPort:               51280,
			WireGuardHost:            "127.0.0.1",
			ProbeTimeoutSeconds:      5,
			KeepaliveIntervalSeconds: 20,
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
			Enabled: false,
			ApiAddr: "localhost:8080",
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
	applyEnvOverrides(cfg)

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
	applyEnvOverrides(cfg)

	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if value := strings.TrimSpace(os.Getenv("SCNET_DISCOVERY_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Discovery.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_DISCOVERY_URL")); value != "" {
		cfg.Discovery.DiscoveryURL = value
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_DISCOVERY_SECRET")); value != "" {
		cfg.Discovery.DiscoverySecret = value
	} else if value := strings.TrimSpace(os.Getenv("SCNET_AGENT_TOKEN")); value != "" && cfg.Discovery.DiscoverySecret == "" {
		cfg.Discovery.DiscoverySecret = value
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_DISCOVERY_API_ADDR")); value != "" {
		cfg.Discovery.ApiAddr = value
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_WG_ENDPOINT")); value != "" {
		cfg.Discovery.WgEndpoint = value
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_PUNCH_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Punch.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_PUNCH_LISTEN_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Punch.ListenPort = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_PUNCH_WG_HOST")); value != "" {
		cfg.Punch.WireGuardHost = value
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_PUNCH_WG_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Punch.WireGuardPort = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_STUN_SERVERS")); value != "" {
		cfg.Punch.STUNServers = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_PUNCH_PROBE_TIMEOUT_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Punch.ProbeTimeoutSeconds = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SCNET_PUNCH_KEEPALIVE_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Punch.KeepaliveIntervalSeconds = parsed
		}
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
