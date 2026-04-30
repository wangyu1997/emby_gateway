package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"emby302/internal/config"
	"emby302/internal/geoip"
	"emby302/internal/proxy"
)

type Server struct {
	cfg      *config.Config
	cfgPath  string
	geoip    *geoip.GeoIP
	proxy    *proxy.EmbyProxy
	cfgMu    sync.Mutex
	username string
	password string
}

func New(cfg *config.Config, cfgPath string, g *geoip.GeoIP, p *proxy.EmbyProxy, username, password string) *Server {
	return &Server{
		cfg:      cfg,
		cfgPath:  cfgPath,
		geoip:    g,
		proxy:    p,
		username: username,
		password: password,
	}
}

func (s *Server) ListenAndServe(port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/config", s.authMiddleware(s.handleConfig))
	mux.HandleFunc("/api/stats", s.authMiddleware(s.handleStats))
	mux.HandleFunc("/api/trackers", s.authMiddleware(s.handleTrackers))
	mux.HandleFunc("/", s.authMiddleware(s.handlePage))

	addr := fmt.Sprintf(":%d", port)
	log.Printf("管理后台启动在 %s", addr)
	return http.ListenAndServe(addr, s.middleware(mux))
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/" {
			next(w, r)
			return
		}

		token := r.Header.Get("Authorization")
		if token == "" || !s.validateToken(token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "未登录"})
			return
		}
		next(w, r)
	}
}

func (s *Server) validateToken(token string) bool {
	expected := s.generateToken()
	return token == expected
}

func (s *Server) generateToken() string {
	h := sha256.Sum256([]byte(s.username + ":" + s.password))
	return hex.EncodeToString(h[:])
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if req.Username == s.username && req.Password == s.password {
		writeJSON(w, map[string]string{
			"token":    s.generateToken(),
			"username": s.username,
		})
	} else {
		writeError(w, "用户名或密码错误", http.StatusUnauthorized)
	}
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(adminPage)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.cfgMu.Lock()
		cfg := *s.cfg
		s.cfgMu.Unlock()
		writeJSON(w, cfg)

	case http.MethodPost:
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			writeError(w, "解析配置失败: "+err.Error(), http.StatusBadRequest)
			return
		}

		s.cfgMu.Lock()
		*s.cfg = newCfg
		s.cfgMu.Unlock()

		if err := saveConfig(s.cfgPath, &newCfg); err != nil {
			writeError(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, map[string]string{"message": "配置已保存"})

	default:
		writeError(w, "不支持的方法", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.geoip == nil {
		writeJSON(w, map[string]interface{}{
			"cache_hits":   0,
			"cache_misses": 0,
			"cache_size":   0,
			"cache_rate":   "0.0",
		})
		return
	}
	hits, misses, size := s.geoip.Stats()
	total := hits + misses
	rate := 0.0
	if total > 0 {
		rate = float64(hits) / float64(total) * 100
	}
	writeJSON(w, map[string]interface{}{
		"cache_hits":   hits,
		"cache_misses": misses,
		"cache_size":   size,
		"cache_rate":   fmt.Sprintf("%.1f", rate),
	})
}

func (s *Server) handleTrackers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.proxy.GetTrackers())
}

func saveConfig(path string, cfg *config.Config) error {
	yamlData := toYAML(cfg)
	return os.WriteFile(path, yamlData, 0644)
}

func toYAML(cfg *config.Config) []byte {
	var b strings.Builder
	b.WriteString("server:\n")
	b.WriteString(fmt.Sprintf("  port: %d\n", cfg.Server.Port))
	b.WriteString(fmt.Sprintf("  admin_port: %d\n", cfg.Server.AdminPort))
	b.WriteString(fmt.Sprintf("  secret: \"%s\"\n", cfg.Server.Secret))
	b.WriteString("\nadmin:\n")
	b.WriteString(fmt.Sprintf("  username: \"%s\"\n", cfg.Admin.Username))
	b.WriteString(fmt.Sprintf("  password: \"%s\"\n", cfg.Admin.Password))
	b.WriteString("\nemby:\n")
	b.WriteString(fmt.Sprintf("  url: \"%s\"\n", cfg.Emby.URL))
	b.WriteString(fmt.Sprintf("  api_key: \"%s\"\n", cfg.Emby.APIKey))
	b.WriteString("\ngeoip:\n")
	b.WriteString(fmt.Sprintf("  db_path: \"%s\"\n", cfg.GeoIP.DBPath))
	b.WriteString(fmt.Sprintf("  server_city: \"%s\"\n", cfg.GeoIP.ServerCity))
	b.WriteString(fmt.Sprintf("  auto_download: %t\n", cfg.GeoIP.AutoDownload))
	b.WriteString(fmt.Sprintf("  auto_update: \"%s\"\n", cfg.GeoIP.AutoUpdate))
	b.WriteString(fmt.Sprintf("  api_fallback_url: \"%s\"\n", cfg.GeoIP.APIFallbackURL))
	b.WriteString(fmt.Sprintf("  ip_cache_ttl: \"%s\"\n", cfg.GeoIP.IPCacheTTL))
	b.WriteString("\nrouting:\n")
	b.WriteString(fmt.Sprintf("  same_city: \"%s\"\n", cfg.Routing.SameCity))
	b.WriteString(fmt.Sprintf("  different_city: \"%s\"\n", cfg.Routing.DifferentCity))
	b.WriteString(fmt.Sprintf("  fallback: \"%s\"\n", cfg.Routing.Fallback))
	return []byte(b.String())
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
