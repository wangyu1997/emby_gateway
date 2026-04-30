package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server" json:"server"`
	Admin   AdminConfig   `yaml:"admin" json:"admin"`
	Emby    EmbyConfig    `yaml:"emby" json:"emby"`
	GeoIP   GeoIPConfig   `yaml:"geoip" json:"geoip"`
	Routing RoutingConfig `yaml:"routing" json:"routing"`
}

type AdminConfig struct {
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
}

type ServerConfig struct {
	Port      int    `yaml:"port" json:"port"`
	AdminPort int    `yaml:"admin_port" json:"admin_port"`
	Secret    string `yaml:"secret" json:"secret"`
}

type EmbyConfig struct {
	URL    string `yaml:"url" json:"url"`
	APIKey string `yaml:"api_key" json:"api_key"`
}

type GeoIPConfig struct {
	DBPath         string `yaml:"db_path" json:"db_path"`
	ServerCity     string `yaml:"server_city" json:"server_city"`
	AutoDownload   bool   `yaml:"auto_download" json:"auto_download"`
	AutoUpdate     string `yaml:"auto_update" json:"auto_update"`
	APIFallbackURL string `yaml:"api_fallback_url" json:"api_fallback_url"`
	IPCacheTTL     string `yaml:"ip_cache_ttl" json:"ip_cache_ttl"`
}

func (g *GeoIPConfig) AutoUpdateDuration() time.Duration {
	d, _ := time.ParseDuration(g.AutoUpdate)
	return d
}

func (g *GeoIPConfig) IPCacheTTLDuration() time.Duration {
	d, _ := time.ParseDuration(g.IPCacheTTL)
	if d == 0 {
		d = 1 * time.Hour
	}
	return d
}

type RoutingConfig struct {
	SameCity      string `yaml:"same_city" json:"same_city"`
	DifferentCity string `yaml:"different_city" json:"different_city"`
	Fallback      string `yaml:"fallback" json:"fallback"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8095
	}
	if cfg.Server.AdminPort == 0 {
		cfg.Server.AdminPort = 8098
	}
	if cfg.Routing.SameCity == "" {
		cfg.Routing.SameCity = "redirect"
	}
	if cfg.Routing.DifferentCity == "" {
		cfg.Routing.DifferentCity = "proxy"
	}
	if cfg.Routing.Fallback == "" {
		cfg.Routing.Fallback = "proxy"
	}

	return &cfg, nil
}
