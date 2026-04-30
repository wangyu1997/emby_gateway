package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Admin   AdminConfig   `yaml:"admin"`
	Emby    EmbyConfig    `yaml:"emby"`
	GeoIP   GeoIPConfig   `yaml:"geoip"`
	Routing RoutingConfig `yaml:"routing"`
}

type AdminConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type ServerConfig struct {
	Port      int    `yaml:"port"`
	AdminPort int    `yaml:"admin_port"`
	Secret    string `yaml:"secret"`
}

type EmbyConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

type GeoIPConfig struct {
	DBPath         string        `yaml:"db_path"`
	ServerCity     string        `yaml:"server_city"`
	AutoDownload   bool          `yaml:"auto_download"`
	AutoUpdate     time.Duration `yaml:"auto_update"`      // 更新间隔，如 "24h"
	APIFallbackURL string        `yaml:"api_fallback_url"` // HTTP API 备用
	IPCacheTTL     time.Duration `yaml:"ip_cache_ttl"`     // IP 缓存时间
}

type RoutingConfig struct {
	SameCity      string `yaml:"same_city"`
	DifferentCity string `yaml:"different_city"`
	Fallback      string `yaml:"fallback"`
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
