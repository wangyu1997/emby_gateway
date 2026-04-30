package config

import (
	"fmt"
	"time"

	"emby302/internal/db"
)

type Config struct {
	Server  ServerConfig  `json:"server"`
	Admin   AdminConfig   `json:"admin"`
	Emby    EmbyConfig    `json:"emby"`
	GeoIP   GeoIPConfig   `json:"geoip"`
	Routing RoutingConfig `json:"routing"`
	Tweaks  TweakConfig   `json:"tweaks"`
}

type AdminConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ServerConfig struct {
	Port      int    `json:"port"`
	AdminPort int    `json:"admin_port"`
	Secret    string `json:"secret"`
}

type EmbyConfig struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

type GeoIPConfig struct {
	DBPath         string `json:"db_path"`
	ServerCity     string `json:"server_city"`
	AutoDownload   bool   `json:"auto_download"`
	AutoUpdate     string `json:"auto_update"`
	APIFallbackURL string `json:"api_fallback_url"`
	IPCacheTTL     string `json:"ip_cache_ttl"`
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
	SameCity      string `json:"same_city"`
	DifferentCity string `json:"different_city"`
	Fallback      string `json:"fallback"`
}

type TweakConfig struct {
	HidePremiere bool `json:"hide_premiere"`
}

const configKey = "main"

func Load(s *db.Store) (*Config, error) {
	cfg := defaultConfig()
	err := s.GetConfig(configKey, cfg)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func Save(s *db.Store, cfg *Config) error {
	return s.SetConfig(configKey, cfg)
}

func defaultConfig() *Config {
	return &Config{
		Server:  ServerConfig{Port: 8095, AdminPort: 8098, Secret: "change-me-to-a-random-secret"},
		Admin:   AdminConfig{Username: "admin", Password: "admin"},
		Emby:    EmbyConfig{URL: "http://127.0.0.1:8096", APIKey: ""},
		GeoIP:   GeoIPConfig{DBPath: "./GeoLite2-City.mmdb", ServerCity: "北京", AutoDownload: true, AutoUpdate: "24h", IPCacheTTL: "1h"},
		Routing: RoutingConfig{SameCity: "redirect", DifferentCity: "proxy", Fallback: "proxy"},
		Tweaks:  TweakConfig{HidePremiere: false},
	}
}
