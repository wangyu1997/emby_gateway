package geoip

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emby302/internal/cache"

	"github.com/oschwald/maxminddb-golang"
)

// Download URLs for GeoLite2-City (free mirrors, no license needed)
var downloadURLs = []string{
	"https://github.com/P3TERX/GeoLite.mmdb/releases/download/GeoLite2-City/GeoLite2-City.mmdb",
	"https://github.com/wp-statistics/GeoLite2-City/raw/master/GeoLite2-City.mmdb",
	"https://cdn.jsdelivr.net/gh/P3TERX/GeoLite.mmdb/GeoLite2-City.mmdb",
}

type GeoIP struct {
	mu         sync.RWMutex
	db         *maxminddb.Reader
	dbPath     string
	serverCity string
	httpClient *http.Client
	apiURL     string      // optional: HTTP API for fallback
	ipCache    *cache.Cache // IP 查询结果缓存池
	cacheTTL   time.Duration

	// 缓存统计
	statsMu    sync.Mutex
	cacheHits  int64
	cacheMisses int64
}

type cityRecord struct {
	Names map[string]string `maxminddb:"names"`
}

type subdivisionRecord struct {
	Names map[string]string `maxminddb:"names"`
}

type geoRecord struct {
	City         cityRecord         `maxminddb:"city"`
	Subdivisions []subdivisionRecord `maxminddb:"subdivisions"`
	Country      cityRecord         `maxminddb:"country"`
}

// API response format for ip-api.com
type apiResult struct {
	Status      string `json:"status"`
	City        string `json:"city"`
	RegionName  string `json:"regionName"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	Message     string `json:"message"`
}

func New(dbPath, serverCity string, autoDownload bool, updateInterval time.Duration, cacheTTL time.Duration) (*GeoIP, error) {
	if cacheTTL <= 0 {
		cacheTTL = 1 * time.Hour
	}
	g := &GeoIP{
		dbPath:     dbPath,
		serverCity: serverCity,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		ipCache:    cache.New(cacheTTL, cacheTTL*2),
		cacheTTL:   cacheTTL,
	}

	// Try to open existing db
	if _, err := os.Stat(dbPath); err == nil {
		if err := g.openDB(); err != nil {
			log.Printf("打开现有 GeoIP 数据库失败: %v", err)
			if autoDownload {
				if err := g.download(); err != nil {
					log.Printf("GeoIP 数据库重新下载失败: %v，将使用兜底策略", err)
					return g, nil // 返回 g，不报错，GeoIP 查询走 fallback
				}
			}
		}
	} else if autoDownload {
		if err := g.download(); err != nil {
			log.Printf("GeoIP 数据库下载失败: %v，将使用兜底策略", err)
			return g, nil // 返回 g，不报错
		}
	}

	// Start periodic update
	if updateInterval > 0 {
		go g.periodicUpdate(updateInterval)
	}

	return g, nil
}

// SetAPI enables HTTP API fallback mode
func (g *GeoIP) SetAPI(apiURL string) {
	g.apiURL = apiURL
}

func (g *GeoIP) LookupCity(ip net.IP) (city, province, country string, err error) {
	// Check IP cache first
	ipStr := ip.String()
	if cached, ok := g.ipCache.Get(ipStr); ok {
		g.statsMu.Lock()
		g.cacheHits++
		g.statsMu.Unlock()
		if entry, ok := cached.(ipEntry); ok {
			return entry.city, entry.province, entry.country, nil
		}
	}

	g.statsMu.Lock()
	g.cacheMisses++
	g.statsMu.Unlock()

	// Try local DB first
	g.mu.RLock()
	db := g.db
	g.mu.RUnlock()

	var lookupErr error
	if db != nil {
		var record geoRecord
		if err := db.Lookup(ip, &record); err == nil {
			city = record.City.Names["zh-CN"]
			if city == "" {
				city = record.City.Names["en"]
			}
			if len(record.Subdivisions) > 0 {
				province = record.Subdivisions[0].Names["zh-CN"]
				if province == "" {
					province = record.Subdivisions[0].Names["en"]
				}
			}
			country = record.Country.Names["zh-CN"]
			if country == "" {
				country = record.Country.Names["en"]
			}
			g.ipCache.Set(ipStr, ipEntry{city: city, province: province, country: country}, g.cacheTTL)
			return city, province, country, nil
		}
		lookupErr = err
	}

	// Fallback to API
	if g.apiURL != "" {
		city, province, country, err = g.lookupByAPI(ip)
		if err == nil {
			g.ipCache.Set(ipStr, ipEntry{city: city, province: province, country: country}, g.cacheTTL)
			return city, province, country, nil
		}
		lookupErr = err
	}

	return "", "", "", fmt.Errorf("geoip lookup failed for %s: %v", ip, lookupErr)
}

type ipEntry struct {
	city, province, country string
}

func (g *GeoIP) IsSameCity(ip net.IP) bool {
	city, _, _, err := g.LookupCity(ip)
	if err != nil {
		return false
	}
	return city == g.serverCity
}

// Stats returns cache hit/miss statistics
func (g *GeoIP) Stats() (hits, misses int64, size int) {
	g.statsMu.Lock()
	hits = g.cacheHits
	misses = g.cacheMisses
	g.statsMu.Unlock()
	g.mu.RLock()
	size = g.ipCache.Len()
	g.mu.RUnlock()
	return
}

func (g *GeoIP) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.db != nil {
		return g.db.Close()
	}
	return nil
}

func (g *GeoIP) openDB() error {
	db, err := maxminddb.Open(g.dbPath)
	if err != nil {
		return err
	}

	g.mu.Lock()
	old := g.db
	g.db = db
	g.mu.Unlock()

	if old != nil {
		old.Close()
	}
	return nil
}

func (g *GeoIP) download() error {
	log.Println("正在下载 GeoIP 数据库...")

	var lastErr error
	for _, url := range downloadURLs {
		if err := g.downloadFrom(url); err == nil {
			log.Printf("GeoIP 数据库下载成功: %s", g.dbPath)
			return g.openDB()
		} else {
			log.Printf("从 %s 下载失败: %v", url, err)
			lastErr = err
		}
	}

	return fmt.Errorf("all download sources failed: %v", lastErr)
}

func (g *GeoIP) downloadFrom(url string) error {
	resp, err := g.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	// Handle .tar.gz
	if strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
		return g.extractTarGz(resp.Body)
	}

	// Direct .mmdb download
	dir := filepath.Dir(g.dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := g.dbPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()

	return os.Rename(tmp, g.dbPath)
}

func (g *GeoIP) extractTarGz(body io.Reader) error {
	gzr, err := gzip.NewReader(body)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	dir := filepath.Dir(g.dbPath)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if strings.HasSuffix(header.Name, ".mmdb") {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}

			tmp := g.dbPath + ".tmp"
			f, err := os.Create(tmp)
			if err != nil {
				return err
			}

			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			f.Close()

			return os.Rename(tmp, g.dbPath)
		}
	}

	return fmt.Errorf("no .mmdb file found in archive")
}

func (g *GeoIP) lookupByAPI(ip net.IP) (city, province, country string, err error) {
	url := fmt.Sprintf(g.apiURL, ip.String())
	resp, err := g.httpClient.Get(url)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	var result apiResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", fmt.Errorf("parse api response: %w", err)
	}

	if result.Status != "success" {
		return "", "", "", fmt.Errorf("api error: %s", result.Message)
	}

	return result.City, result.RegionName, result.Country, nil
}

func (g *GeoIP) periodicUpdate(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("开始更新 GeoIP 数据库...")
		if err := g.download(); err != nil {
			log.Printf("GeoIP 数据库更新失败: %v", err)
		} else {
			log.Println("GeoIP 数据库更新成功")
		}
	}
}
