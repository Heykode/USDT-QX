package authguard

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReserveBeforeConcurrentAttemptsAndExpiry(t *testing.T) {
	l := New()
	now := time.Now()
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("192.0.2.1", "Admin", now) == 0 {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 12 {
		t.Fatal("concurrent budget bypass", count.Load())
	}
	if l.Allow("192.0.2.2", " admin ", now) == 0 {
		t.Fatal("IP rotation bypassed account limit")
	}
	if l.Allow("192.0.2.1", "admin", now.Add(16*time.Minute)) != 0 {
		t.Fatal("expired limit did not release")
	}
}
func TestProxyIdentityCannotTrustClientHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:4000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if ClientIP(r, "") != "192.0.2.1" {
		t.Fatal("untrusted header used")
	}
	r.RemoteAddr = "127.0.0.1:4000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.8")
	if ClientIP(r, "127.0.0.1/32") != "198.51.100.8" {
		t.Fatal("spoofed leftmost forwarding address used")
	}
}
