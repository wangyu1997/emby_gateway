package proxy

import (
	"bytes"
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
	client *http.Client // 用于 Emby 转发（不跟随重定向）
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
		cfg:    cfg,
		geoip:  g,
		client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		pCache:   cache.New(5*time.Minute, 10*time.Minute),
		trackers: make(map[string]*ClientTracker),
	}
}

// streamClient 用于代理流，自动跟随重定向
func (p *EmbyProxy) streamClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
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

func (p *EmbyProxy) FlushCache() {
	p.pCache.Flush()
}

func (p *EmbyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 处理 CORS 预检请求
	if r.Method == http.MethodOptions && strings.HasPrefix(r.URL.Path, "/proxy/stream/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Range, Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Content-Type, Accept-Ranges")
		w.WriteHeader(http.StatusOK)
		return
	}

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

	// 拦截 HTML 页面，注入自定义样式
	if strings.HasSuffix(r.URL.Path, "/web/index.html") || strings.HasSuffix(r.URL.Path, "/web/") {
		p.handleWebPage(w, r)
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
	forwardReq.ContentLength = r.ContentLength

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
		mediaURL := source.Path
		if mediaURL == "" {
			continue
		}

		// 判断是否为 STRM 文件：Protocol="Http" 表示远程流，Path 为 http URL 也是 STRM
		isSTRM := source.Protocol == "Http" || isHTTPURL(mediaURL)
		if !isSTRM {
			// 本地视频：Emby 的 DirectStreamUrl 会指向 /videos/{id}/stream，自然走代理转发
			// 不做任何修改，交由 Emby 正常处理
			continue
		}

		// STRM 视频：根据 IP 策略决定路由
		if decision.strategy == "redirect" {
			// 同城：返回 CDN 直链，客户端直接访问
			source.DirectStreamURL = mediaURL
		} else {
			// 异地：替换为本地代理 URL，流量经服务器中转
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
		log.Printf("proxy stream: decode error: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	mediaURL := string(rawURL)

	token := r.URL.Query().Get("token")
	if !p.validateToken(mediaURL, token) {
		log.Printf("proxy stream: token invalid")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	proxyReq, err := http.NewRequest("GET", mediaURL, nil)
	if err != nil {
		log.Printf("proxy stream: create request error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	if rng := r.Header.Get("Range"); rng != "" {
		proxyReq.Header.Set("Range", rng)
	}
	proxyReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	log.Printf("proxy stream: %s range=%s", mediaURL, r.Header.Get("Range"))

	proxyResp, err := p.streamClient().Do(proxyReq)
	if err != nil {
		log.Printf("proxy stream: request error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer proxyResp.Body.Close()

	log.Printf("proxy stream: response status=%d content-type=%s content-length=%d",
		proxyResp.StatusCode, proxyResp.Header.Get("Content-Type"), proxyResp.ContentLength)

	// 先复制上游响应头，再设置 CORS 头（确保不被覆盖）
	for k, v := range proxyResp.Header {
		w.Header()[k] = v
	}

	// 设置 CORS 头，允许浏览器跨域播放
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range, Content-Type")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Content-Type, Accept-Ranges")

	w.WriteHeader(proxyResp.StatusCode)

	io.Copy(w, proxyResp.Body)
}

func (p *EmbyProxy) handlePlaying(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		clientIP := extractClientIP(r)
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			p.forwardToEmby(w, r)
			return
		}
		// 恢复 Body 和 ContentLength 供后续转发使用
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		r.ContentLength = int64(len(bodyBytes))

		var body map[string]interface{}
		json.Unmarshal(bodyBytes, &body)
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
						IP:       clientIP,
						ItemID:   itemID,
						ItemName: itemName,
						Started:  time.Now().Format("15:04:05"),
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

	// 手动设置 ContentLength，确保 body 正确发送
	if forwardReq.ContentLength <= 0 && r.ContentLength > 0 {
		forwardReq.ContentLength = r.ContentLength
	}

	// 确保 POST 请求使用 application/json，避免 Sessions/Playing 返回 400
	if r.Method == http.MethodPost {
		ct := forwardReq.Header.Get("Content-Type")
		if ct == "" || strings.HasPrefix(ct, "text/plain") {
			forwardReq.Header.Set("Content-Type", "application/json")
		}
	}

	// 修正 Host 头为 Emby 实际地址
	embyHost := strings.TrimPrefix(p.cfg.Emby.URL, "http://")
	embyHost = strings.TrimPrefix(embyHost, "https://")
	forwardReq.Host = embyHost

	// 添加代理转发标准头，Emby 依赖这些头构造正确的资源 URL
	forwardReq.Header.Set("X-Forwarded-For", extractClientIP(r))
	forwardReq.Header.Set("X-Forwarded-Proto", scheme(r))
	forwardReq.Header.Set("X-Forwarded-Host", r.Host)

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

func (p *EmbyProxy) handleWebPage(w http.ResponseWriter, r *http.Request) {
	if !p.cfg.Tweaks.HidePremiere {
		p.forwardToEmby(w, r)
		return
	}
	p.forwardToEmbyBuffered(w, r, func(body []byte) []byte {
		inject := `<style>
.btnPremiere,[class*="btnPremiere"],[data-id="premiere"],.card-premiere,.section-premiere,
[class*="card-premiere"],[class*="section-premiere"],.dashboardFooter,
a[href*="premiere"],.btn-emby-premiere,.promo-card-premiere,
.section-promo,.card-promo,[class*="promo"]{display:none!important}
</style>`
		return bytes.ReplaceAll(body, []byte("</head>"), append([]byte(inject), []byte("</head>")...))
	})
}

func (p *EmbyProxy) forwardToEmbyBuffered(w http.ResponseWriter, r *http.Request, transform func([]byte) []byte) {
	embyURL := p.cfg.Emby.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		embyURL += "?" + r.URL.RawQuery
	}

	// GET 请求不需要 body
	var bodyReader io.Reader
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		bodyReader = r.Body
	}

	forwardReq, err := http.NewRequest(r.Method, embyURL, bodyReader)
	if err != nil {
		log.Printf("forwardToEmbyBuffered: create request error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	forwardReq.Header = r.Header.Clone()

	// 关键：删除 Accept-Encoding，让 Go 自动解压 gzip
	// 如果保留原始请求的 Accept-Encoding，Go 不会自动解压
	forwardReq.Header.Del("Accept-Encoding")

	if r.ContentLength > 0 {
		forwardReq.ContentLength = r.ContentLength
	}

	if r.Method == http.MethodPost {
		ct := forwardReq.Header.Get("Content-Type")
		if ct == "" || strings.HasPrefix(ct, "text/plain") {
			forwardReq.Header.Set("Content-Type", "application/json")
		}
	}

	embyHost := strings.TrimPrefix(p.cfg.Emby.URL, "http://")
	embyHost = strings.TrimPrefix(embyHost, "https://")
	forwardReq.Host = embyHost

	forwardReq.Header.Set("X-Forwarded-For", extractClientIP(r))
	forwardReq.Header.Set("X-Forwarded-Proto", scheme(r))
	forwardReq.Header.Set("X-Forwarded-Host", r.Host)

	log.Printf("forwardToEmbyBuffered: GET %s", embyURL)

	resp, err := p.client.Do(forwardReq)
	if err != nil {
		log.Printf("forwardToEmbyBuffered: request error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("forwardToEmbyBuffered: upstream status=%d content-type=%s", resp.StatusCode, resp.Header.Get("Content-Type"))

	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("forwardToEmbyBuffered: read error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	log.Printf("forwardToEmbyBuffered: read %d bytes, transforming", len(respBody))

	respBody = transform(respBody)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
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

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
