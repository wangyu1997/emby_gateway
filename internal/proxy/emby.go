package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"emby302/internal/cache"
	"emby302/internal/config"
	"emby302/internal/geoip"
	"emby302/pkg/models"
)

type EmbyProxy struct {
	cfg    *config.Config
	geoip  *geoip.GeoIP
	client *http.Client
	pCache *cache.Cache

	// 客户端连接追踪
	trackersMu sync.Mutex
	trackers   map[string]*ClientTracker
}

type ClientTracker struct {
	IP       string `json:"ip"`
	City     string `json:"city"`
	Province string `json:"province"`
	Strategy string `json:"strategy"`
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	MediaURL string `json:"media_url"`
	Started  string `json:"started"`
}

func New(cfg *config.Config, g *geoip.GeoIP) *EmbyProxy {
	return &EmbyProxy{
		cfg:      cfg,
		geoip:    g,
		client:   &http.Client{Timeout: 60 * time.Second},
		pCache:   cache.New(5*time.Minute, 10*time.Minute),
		trackers: make(map[string]*ClientTracker),
	}
}

func (p *EmbyProxy) GetTrackers() []ClientTracker {
	p.trackersMu.Lock()
	defer p.trackersMu.Unlock()
	result := make([]ClientTracker, 0, len(p.trackers))
	for _, t := range p.trackers {
		result = append(result, *t)
	}
	return result
}

func (p *EmbyProxy) RemoveTracker(key string) {
	p.trackersMu.Lock()
	delete(p.trackers, key)
	p.trackersMu.Unlock()
}

func (p *EmbyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/PlaybackInfo") {
		p.handlePlaybackInfo(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/proxy/stream/") {
		p.handleProxyStream(w, r)
		return
	}

	// 追踪客户端连接（Sessions/Playing/Progress 等）
	if strings.Contains(r.URL.Path, "/Sessions/Playing") {
		p.handlePlaying(w, r)
		return
	}

	p.forwardToEmby(w, r)
}

func (p *EmbyProxy) handlePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	clientIP := extractClientIP(r)
	cacheKey := clientIP + ":" + r.URL.Path

	if cached, ok := p.pCache.Get(cacheKey); ok {
		if body, ok := cached.([]byte); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
			return
		}
	}

	// Forward to Emby
	embyURL := p.cfg.Emby.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		embyURL += "?" + r.URL.RawQuery
	}

	forwardReq, err := http.NewRequest(r.Method, embyURL, r.Body)
	if err != nil {
		log.Printf("create emby request: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	forwardReq.Header = r.Header.Clone()

	resp, err := p.client.Do(forwardReq)
	if err != nil {
		log.Printf("emby request: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("read emby response: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Parse and modify
	var playback models.PlaybackInfoResponse
	if err := json.Unmarshal(body, &playback); err != nil {
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	decision := p.makeRoutingDecision(r, clientIP)

	for i := range playback.MediaSources {
		source := &playback.MediaSources[i]
		// Path 就是 STRM 文件中的原始 URL
		mediaURL := source.Path
		if mediaURL == "" {
			continue
		}

		if decision.strategy == "redirect" {
			// 同城：直接返回原始 URL，客户端直连网盘 CDN
			source.DirectStreamURL = mediaURL
		} else {
			// 异地：替换为本地代理 URL
			token := p.generateToken(mediaURL)
			encoded := base64.URLEncoding.EncodeToString([]byte(mediaURL))
			source.DirectStreamURL = fmt.Sprintf(
				"%s/proxy/stream/%s?token=%s",
				baseURL(r),
				encoded,
				token,
			)
		}
	}

	modifiedBody, _ := json.Marshal(playback)

	w.Header().Set("Content-Type", "application/json")
	w.Write(modifiedBody)
	p.pCache.Set(cacheKey, modifiedBody, 5*time.Minute)

	// 记录追踪信息
	p.trackersMu.Lock()
	trackKey := clientIP + ":" + decision.city
	p.trackers[trackKey] = &ClientTracker{
		IP:       clientIP,
		City:     decision.city,
		Province: decision.province,
		Strategy: decision.strategy,
		MediaURL: playback.MediaSources[0].Path,
		Started:  time.Now().Format("15:04:05"),
	}
	p.trackersMu.Unlock()

	log.Printf("PlaybackInfo: ip=%s city=%s strategy=%s",
		clientIP, decision.city, decision.strategy)
}

func (p *EmbyProxy) handleProxyStream(w http.ResponseWriter, r *http.Request) {
	encoded := strings.TrimPrefix(r.URL.Path, "/proxy/stream/")
	rawURL, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	mediaURL := string(rawURL)

	token := r.URL.Query().Get("token")
	if !p.validateToken(mediaURL, token) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	proxyReq, err := http.NewRequest("GET", mediaURL, nil)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	if rng := r.Header.Get("Range"); rng != "" {
		proxyReq.Header.Set("Range", rng)
	}
	proxyReq.Header.Set("User-Agent", "Mozilla/5.0")

	proxyResp, err := p.client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer proxyResp.Body.Close()

	for k, v := range proxyResp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(proxyResp.StatusCode)

	io.Copy(w, proxyResp.Body)
}

func (p *EmbyProxy) handlePlaying(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// 记录播放开始
		clientIP := extractClientIP(r)
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if item, ok := body["Item"]; ok {
			if itemMap, ok := item.(map[string]interface{}); ok {
				itemID, _ := itemMap["Id"].(string)
				itemName, _ := itemMap["Name"].(string)
				p.trackersMu.Lock()
				trackKey := clientIP + ":playing"
				if t, exists := p.trackers[trackKey]; exists {
					t.ItemID = itemID
					t.ItemName = itemName
					t.Started = time.Now().Format("15:04:05")
				} else {
					p.trackers[trackKey] = &ClientTracker{
						IP:      clientIP,
						ItemID:  itemID,
						ItemName: itemName,
						Started: time.Now().Format("15:04:05"),
					}
				}
				p.trackersMu.Unlock()
			}
		}
	}
	p.forwardToEmby(w, r)
}

func (p *EmbyProxy) forwardToEmby(w http.ResponseWriter, r *http.Request) {
	embyURL := p.cfg.Emby.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		embyURL += "?" + r.URL.RawQuery
	}

	forwardReq, err := http.NewRequest(r.Method, embyURL, r.Body)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	forwardReq.Header = r.Header.Clone()

	resp, err := p.client.Do(forwardReq)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (p *EmbyProxy) makeRoutingDecision(r *http.Request, clientIP string) *routingDecision {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return &routingDecision{clientIP: clientIP, strategy: p.cfg.Routing.Fallback}
	}

	if p.geoip == nil {
		return &routingDecision{clientIP: clientIP, strategy: p.cfg.Routing.Fallback}
	}

	city, province, _, err := p.geoip.LookupCity(ip)
	if err != nil {
		return &routingDecision{clientIP: clientIP, strategy: p.cfg.Routing.Fallback}
	}

	isSame := city == p.cfg.GeoIP.ServerCity
	strategy := p.cfg.Routing.DifferentCity
	if isSame {
		strategy = p.cfg.Routing.SameCity
	}

	return &routingDecision{clientIP: clientIP, city: city, province: province, strategy: strategy}
}

func (p *EmbyProxy) generateToken(mediaURL string) string {
	h := hmac.New(sha256.New, []byte(p.cfg.Server.Secret))
	h.Write([]byte(mediaURL))
	h.Write([]byte(time.Now().Format("2006-01-02")))
	return hex.EncodeToString(h.Sum(nil))
}

func (p *EmbyProxy) validateToken(mediaURL, token string) bool {
	expected := p.generateToken(mediaURL)
	return hmac.Equal([]byte(token), []byte(expected))
}

type routingDecision struct {
	clientIP string
	city     string
	province string
	strategy string
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
