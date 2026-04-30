package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"emby302/internal/admin"
	"emby302/internal/config"
	"emby302/internal/db"
	"emby302/internal/geoip"
	"emby302/internal/proxy"
)

func main() {
	dataDir := flag.String("data-dir", ".", "数据目录，存放数据库文件和GeoIP数据库")
	flag.Parse()

	dbPath := filepath.Join(*dataDir, "config.db")
	store, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	cfg, err := config.Load(store)
	if err != nil {
		// 首次使用，初始化默认配置
		cfg = &config.Config{
			Server:  config.ServerConfig{Port: 8095, AdminPort: 8098, Secret: "change-me-to-a-random-secret"},
			Admin:   config.AdminConfig{Username: "admin", Password: "admin"},
			Emby:    config.EmbyConfig{URL: "http://127.0.0.1:8096"},
			GeoIP:   config.GeoIPConfig{DBPath: "./GeoLite2-City.mmdb", ServerCity: "北京", AutoDownload: true, AutoUpdate: "24h", IPCacheTTL: "1h"},
			Routing: config.RoutingConfig{SameCity: "redirect", DifferentCity: "proxy", Fallback: "proxy"},
		}
		config.Save(store, cfg)
		log.Println("已初始化默认配置")
	}

	updateInterval := cfg.GeoIP.AutoUpdateDuration()
	ipCacheTTL := cfg.GeoIP.IPCacheTTLDuration()

	geoPath := cfg.GeoIP.DBPath
	if !filepath.IsAbs(geoPath) {
		geoPath = filepath.Join(*dataDir, geoPath)
	}

	g, err := geoip.New(geoPath, cfg.GeoIP.ServerCity, cfg.GeoIP.AutoDownload, updateInterval, ipCacheTTL)
	if err != nil {
		log.Printf("初始化 GeoIP 失败: %v（代理将继续启动）", err)
		g = nil
	} else {
		defer g.Close()
	}

	if g != nil && cfg.GeoIP.APIFallbackURL != "" {
		g.SetAPI(cfg.GeoIP.APIFallbackURL)
		log.Printf("GeoIP API 备用已启用: %s", cfg.GeoIP.APIFallbackURL)
	}

	if g != nil {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				hits, misses, size := g.Stats()
				total := hits + misses
				rate := float64(0)
				if total > 0 {
					rate = float64(hits) / float64(total) * 100
				}
				log.Printf("GeoIP 缓存统计: 命中=%d 未命中=%d 命中率=%.1f%% 缓存数=%d", hits, misses, rate, size)
			}
		}()
	}

	// 启动代理服务
	p := proxy.New(cfg, g)
	proxyAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	go func() {
		log.Printf("代理服务启动在 %s", proxyAddr)
		log.Fatal(http.ListenAndServe(proxyAddr, p))
	}()

	// 启动管理后台
	adminSrv := admin.New(cfg, store, g, p)
	if err := adminSrv.ListenAndServe(cfg.Server.AdminPort); err != nil {
		log.Fatalf("管理后台启动失败: %v", err)
	}
}

func ensureDefaultConfig(path string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)

	content := `server:
  port: 8095
  admin_port: 8098
  secret: "change-me-to-a-random-secret"

admin:
  username: "admin"
  password: "admin"

emby:
  url: "http://127.0.0.1:8096"
  api_key: ""

geoip:
  db_path: "./GeoLite2-City.mmdb"
  server_city: "北京"
  auto_download: true
  auto_update: "24h"
  api_fallback_url: ""
  ip_cache_ttl: "1h"

routing:
  same_city: "redirect"
  different_city: "proxy"
  fallback: "proxy"
`
	os.WriteFile(path, []byte(content), 0644)
	log.Printf("已生成默认配置文件: %s", path)
}
