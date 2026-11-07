package authguard

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	count   int
	expires time.Time
}
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
}

func New() *Limiter { return &Limiter{buckets: make(map[string]bucket)} }

// Reserve attempts before password hashing, avoiding concurrent guess bursts.
// Counts are process-local; exhausted capacity rejects new keys, never resets
// other clients' blocks. Account limits also cover guesses spread across IPs.
func (l *Limiter) Allow(ip, account string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if !now.Before(b.expires) {
			delete(l.buckets, k)
		}
	}
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	keys := []string{"ip:" + ip, "account:" + hex.EncodeToString(hash[:])}
	limits := []int{40, 12}
	for i, k := range keys {
		if b, ok := l.buckets[k]; ok && b.count >= limits[i] {
			return b.expires.Sub(now)
		}
		if _, ok := l.buckets[k]; !ok && len(l.buckets) >= 8192 {
			return time.Minute
		}
	}
	for _, k := range keys {
		b := l.buckets[k]
		if b.expires.IsZero() {
			b.expires = now.Add(15 * time.Minute)
		}
		b.count++
		l.buckets[k] = b
	}
	return 0
}

func (l *Limiter) ResetAccount(account string) {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, "account:"+hex.EncodeToString(hash[:]))
}

// Trust forwarding headers only from explicitly configured proxy CIDRs. Walk
// right-to-left so a client-supplied leftmost XFF cannot become a new identity.
func ClientIP(r *http.Request, trusted string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	var networks []*net.IPNet
	for _, s := range strings.Split(trusted, ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(s))
		if err == nil {
			networks = append(networks, network)
		}
	}
	isTrusted := func(ip net.IP) bool {
		for _, n := range networks {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !isTrusted(ip) {
		return ip.String()
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(chain) > 32 {
		return ip.String()
	}
	for i := len(chain) - 1; i >= 0 && isTrusted(ip); i-- {
		next := net.ParseIP(strings.TrimSpace(chain[i]))
		if next == nil {
			return ip.String()
		}
		ip = next
	}
	return ip.String()
}
