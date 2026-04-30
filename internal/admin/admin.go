package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"emby302/internal/config"
	"emby302/internal/db"
	"emby302/internal/geoip"
	"emby302/internal/proxy"
)

type Server struct {
	cfg    *config.Config
	store  *db.Store
	geoip  *geoip.GeoIP
	proxy  *proxy.EmbyProxy
	cfgMu  sync.Mutex
}

func New(cfg *config.Config, store *db.Store, g *geoip.GeoIP, p *proxy.EmbyProxy) *Server {
	return &Server{
		cfg:   cfg,
		store: store,
		geoip: g,
		proxy: p,
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
	s.cfgMu.Lock()
	username := s.cfg.Admin.Username
	password := s.cfg.Admin.Password
	s.cfgMu.Unlock()

	h := sha256.Sum256([]byte(username + ":" + password))
	expected := hex.EncodeToString(h[:])
	return token == expected
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

	s.cfgMu.Lock()
	username := s.cfg.Admin.Username
	password := s.cfg.Admin.Password
	s.cfgMu.Unlock()

	if req.Username == username && req.Password == password {
		h := sha256.Sum256([]byte(username + ":" + password))
		writeJSON(w, map[string]string{
			"token":    hex.EncodeToString(h[:]),
			"username": username,
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

		if err := config.Save(s.store, &newCfg); err != nil {
			writeError(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		s.cfgMu.Lock()
		*s.cfg = newCfg
		s.cfgMu.Unlock()

		writeJSON(w, map[string]string{"message": "配置已保存"})

	default:
		writeError(w, "不支持的方法", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.geoip == nil {
		writeJSON(w, map[string]interface{}{
			"cache_hits": 0, "cache_misses": 0, "cache_size": 0, "cache_rate": "0.0",
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

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
