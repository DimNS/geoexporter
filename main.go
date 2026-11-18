package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/oschwald/geoip2-golang"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultDBURL    = "https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-Country.mmdb"
	collectInterval = 15 * time.Second
	updateInterval  = 24 * time.Hour
)

var httpClient = &http.Client{Timeout: 2 * time.Minute}

type geoDB struct {
	mu sync.RWMutex
	r  *geoip2.Reader
}

func (db *geoDB) swap(r *geoip2.Reader) {
	db.mu.Lock()
	old := db.r
	db.r = r
	db.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

func (db *geoDB) open(path string) error {
	r, err := geoip2.Open(path)
	if err != nil {
		return err
	}
	db.swap(r)
	return nil
}

func (db *geoDB) country(ip net.IP) string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.r == nil {
		return "UNKNOWN"
	}
	rec, err := db.r.Country(ip)
	if err != nil || rec.Country.IsoCode == "" {
		return "UNKNOWN"
	}
	return rec.Country.IsoCode
}

// ensure loads the existing database or downloads one if it is missing/corrupt.
func (db *geoDB) ensure(url, path string) error {
	if err := db.open(path); err == nil {
		return nil
	} else {
		log.Printf("cannot load %s: %v; downloading", path, err)
	}
	return db.update(url, path)
}

// update downloads to a temp file, validates it and atomically replaces the old
// database, so lookups keep working during the update.
func (db *geoDB) update(url, path string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".geoip-*.mmdb")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}

	r, err := geoip2.Open(tmpName)
	if err != nil {
		return fmt.Errorf("downloaded database is invalid: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		r.Close()
		return err
	}
	db.swap(r)
	return nil
}

func collect(db *geoDB) (map[string]int, error) {
	out, err := exec.Command("ss", "-H", "-tun", "state", "established").Output()
	if err != nil {
		return nil, fmt.Errorf("ss: %w", err)
	}

	counts := make(map[string]int)
	for _, line := range strings.Split(string(out), "\n") {
		if ip := peerIP(line); ip != nil {
			counts[db.country(ip)]++
		}
	}
	return counts, nil
}

// peerIP extracts the peer address from one `ss -tun` line.
func peerIP(line string) net.IP {
	fields := strings.Fields(line)
	// With a state filter ss omits the State column, so there may be 5 fields.
	if len(fields) < 5 {
		return nil
	}
	peer := fields[len(fields)-1]
	host, _, err := net.SplitHostPort(peer)
	if err != nil {
		// ss may print IPv6 without brackets.
		i := strings.LastIndexByte(peer, ':')
		if i < 0 {
			return nil
		}
		host = strings.Trim(peer[:i], "[]")
	}
	return net.ParseIP(host)
}

func collectInto(db *geoDB, gauge *prometheus.GaugeVec) {
	counts, err := collect(db)
	if err != nil {
		log.Printf("collect: %v", err)
		return
	}
	gauge.Reset()
	for country, n := range counts {
		gauge.WithLabelValues(country).Set(float64(n))
	}
}

func main() {
	listen := flag.String("listen", ":9123", "metrics listen address")
	dbPath := flag.String("db", "/usr/share/GeoIP/GeoLite2-Country.mmdb", "path to GeoLite2-Country.mmdb")
	dbURL := flag.String("url", defaultDBURL, "database download URL")
	flag.Parse()

	db := &geoDB{}
	if err := db.ensure(*dbURL, *dbPath); err != nil {
		log.Fatalf("geoip database: %v", err)
	}

	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "geo_active_connections_by_country",
		Help: "Active connections by country",
	}, []string{"country"})
	prometheus.MustRegister(gauge)

	s, err := gocron.NewScheduler(gocron.WithLocation(time.UTC))
	if err != nil {
		log.Fatalf("scheduler: %v", err)
	}
	if _, err := s.NewJob(gocron.DurationJob(updateInterval), gocron.NewTask(func() {
		if err := db.update(*dbURL, *dbPath); err != nil {
			log.Printf("database update failed: %v", err)
			return
		}
		log.Printf("database updated from %s", *dbURL)
	})); err != nil {
		log.Fatalf("schedule database update: %v", err)
	}
	s.Start()

	go func() {
		for {
			collectInto(db, gauge)
			time.Sleep(collectInterval)
		}
	}()

	log.Printf("listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, promhttp.Handler()))
}
