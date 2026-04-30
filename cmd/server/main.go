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
	"emby302/internal/geoip"
	"emby302/internal/proxy"
)

func main() {
	dataDir := flag.String("data-dir", ".", "数据目录，存放配置文件和GeoIP数据库")
	flag.Parse()

	cfgPath := filepath.Join(*dataDir, "config.yaml")
	ensureDefaultConfig(cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	autoDownload := cfg.GeoIP.AutoDownload
	updateInterval := cfg.GeoIP.AutoUpdate

	dbPath := cfg.GeoIP.DBPath
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(*dataDir, dbPath)
	}

	g, err := geoip.New(dbPath, cfg.GeoIP.ServerCity, autoDownload, updateInterval, cfg.GeoIP.IPCacheTTL)
	if err != nil {
		log.Fatalf("初始化 GeoIP 失败: %v", err)
	}
	defer g.Close()

	if cfg.GeoIP.APIFallbackURL != "" {
		g.SetAPI(cfg.GeoIP.APIFallbackURL)
		log.Printf("GeoIP API 备用已启用: %s", cfg.GeoIP.APIFallbackURL)
	}

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

	// 启动代理服务
	p := proxy.New(cfg, g)
	proxyAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	go func() {
		log.Printf("代理服务启动在 %s", proxyAddr)
		log.Fatal(http.ListenAndServe(proxyAddr, p))
	}()

	// 启动管理后台
	adminSrv := admin.New(cfg, cfgPath, g, p, cfg.Admin.Username, cfg.Admin.Password)
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
