package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

// netFiles are the /proc/net socket tables counted by collect.
var netFiles = []string{"tcp", "tcp6", "udp", "udp6"}

// collect counts established sockets of the host network namespace by the
// country of their peer address. The tables are read from pid 1 of proc, which
// is the host init even when the exporter runs in a container.
func collect(db *geoDB, proc string) (map[string]int, error) {
	dir := filepath.Join(proc, "1", "net")
	counts := make(map[string]int)
	read := 0
	for _, name := range netFiles {
		peers, err := peerIPs(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, ip := range peers {
			counts[db.country(ip)]++
		}
		read++
	}
	if read == 0 {
		// Refuse to report an empty network namespace as "no connections".
		return nil, fmt.Errorf("%s: no socket tables, is the host /proc mounted?", dir)
	}
	return counts, nil
}

func peerIPs(path string) ([]net.IP, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []net.IP
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if ip := peerIP(sc.Text()); ip != nil {
			out = append(out, ip)
		}
	}
	return out, sc.Err()
}

// peerIP extracts the peer address of an established socket from one line of
// a /proc/net table.
func peerIP(line string) net.IP {
	fields := strings.Fields(line)
	// sl local_address rem_address st ...; the header has no hex state.
	if len(fields) < 4 || fields[3] != "01" {
		return nil
	}
	i := strings.LastIndexByte(fields[2], ':')
	if i < 0 {
		return nil
	}
	return decodeIP(fields[2][:i])
}

// decodeIP decodes the /proc/net address format: IPv4 as 4 bytes and IPv6 as
// 4 words of 4 bytes, both little-endian.
func decodeIP(s string) net.IP {
	if len(s) != 8 && len(s) != 32 {
		return nil
	}
	b := make(net.IP, len(s)/2)
	for w := range len(b) / 4 {
		for i := range 4 {
			v, err := strconv.ParseUint(s[w*8+i*2:w*8+i*2+2], 16, 8)
			if err != nil {
				return nil
			}
			b[w*4+3-i] = byte(v)
		}
	}
	return b
}

func collectInto(db *geoDB, gauge *prometheus.GaugeVec, proc string) {
	counts, err := collect(db, proc)
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
	procPath := flag.String("proc", "/proc", "host /proc mount, socket tables are read from <proc>/1/net")
	flag.Parse()
	log.Printf("socket tables: %s", filepath.Join(*procPath, "1", "net"))

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
			collectInto(db, gauge, *procPath)
			time.Sleep(collectInterval)
		}
	}()

	log.Printf("listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, promhttp.Handler()))
}
