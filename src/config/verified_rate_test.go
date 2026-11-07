package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type verifiedFixture struct {
	now                        time.Time
	cfg                        VerifiedRateConfig
	mu                         sync.Mutex
	stored                     VerifiedRateState
	hasState, failSave         bool
	calls, alerts              atomic.Int32
	primary, check             float64
	quoteTime                  time.Time
	primaryStatus, checkStatus int
	primaryBody, checkBody     string
}

func setupVerified(t *testing.T) *verifiedFixture {
	t.Helper()
	f := &verifiedFixture{now: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), primary: 7, check: 7.01, primaryStatus: 200, checkStatus: 200}
	f.quoteTime = f.now
	f.cfg = DefaultVerifiedRateConfig()
	f.cfg.Enabled = true
	f.cfg.CMCAPIKey = "test-cmc-key"
	f.cfg.CoinGeckoAPIKey = "test-cg-key"
	f.cfg.MinPrice = 5
	f.cfg.MaxPrice = 9
	f.cfg.ReferencePrice = 7
	oldGetter, oldNow := SettingsGetString, rateNow
	oldLoad, oldSave, oldAlert := VerifiedRateLoad, VerifiedRateSave, VerifiedRateAlert
	SettingsGetString = func(key string) string {
		if key == "rate.mode" {
			return "auto"
		}
		if key == VerifiedRateSettingKey {
			f.mu.Lock()
			defer f.mu.Unlock()
			raw, _ := json.Marshal(f.cfg)
			return string(raw)
		}
		return ""
	}
	rateNow = func() time.Time { return f.now }
	VerifiedRateLoad = func(base string) (VerifiedRateState, bool, error) { return f.stored, f.hasState, nil }
	VerifiedRateSave = func(s VerifiedRateState) error {
		if f.failSave {
			return errors.New("database write failed")
		}
		f.stored = s
		f.hasState = true
		return nil
	}
	VerifiedRateAlert = func(string) { f.alerts.Add(1) }
	ResetRateCacheRuntime()
	t.Cleanup(func() {
		SettingsGetString = oldGetter
		rateNow = oldNow
		VerifiedRateLoad = oldLoad
		VerifiedRateSave = oldSave
		VerifiedRateAlert = oldAlert
		ResetRateCacheRuntime()
	})
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		f.calls.Add(1)
		status, body := f.primaryStatus, f.primaryBody
		switch r.URL.Host {
		case "pro-api.coinmarketcap.com":
			if r.URL.Path != "/v3/cryptocurrency/quotes/latest" || r.URL.Query().Get("id") != "825" || r.URL.Query().Get("convert") != strings.ToUpper(f.cfg.Currency) || r.Header.Get("X-CMC_PRO_API_KEY") != "test-cmc-key" {
				t.Error("unexpected CMC request shape")
			}
			if body == "" {
				body = fmt.Sprintf(`{"status":{"error_code":0},"data":[{"id":825,"symbol":"USDT","quote":[{"symbol":%q,"price":%g,"last_updated":%q}]}]}`, strings.ToUpper(f.cfg.Currency), f.primary, f.quoteTime.Format(time.RFC3339))
			}
		case "pro-api.coingecko.com":
			status, body = f.checkStatus, f.checkBody
			if r.URL.Path != "/api/v3/simple/price" || r.URL.Query().Get("ids") != "tether" || r.URL.Query().Get("precision") != "full" || r.Header.Get("x-cg-pro-api-key") != "test-cg-key" {
				t.Error("unexpected CoinGecko request shape")
			}
			if body == "" {
				body = fmt.Sprintf(`{"tether":{%q:%g,"last_updated_at":%d}}`, f.cfg.Currency, f.check, f.quoteTime.Unix())
			}
		default:
			t.Errorf("unexpected provider host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return f
}
func TestVerifiedAuditConcurrentReuseAndRestart(t *testing.T) {
	f := setupVerified(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rate, audit := GetPaymentRate("USDT", "CNY")
			if rate != 1.0/7 || !strings.Contains(audit, `"auto_verified"`) {
				t.Errorf("incorrect verified decision: %g", rate)
			}
			if strings.Contains(audit, "test-cmc-key") || strings.Contains(audit, "test-cg-key") {
				t.Error("audit leaked credentials")
			}
		}()
	}
	wg.Wait()
	if f.calls.Load() != 2 {
		t.Fatalf("provider calls=%d", f.calls.Load())
	}
	ResetRateCacheRuntime()
	if GetRateForCoin("usdt", "cny") != 1.0/7 || f.calls.Load() != 2 {
		t.Fatal("persisted quote not reused")
	}
	b, _ := json.Marshal(GetVerifiedRateStatus())
	if strings.Contains(string(b), "test-cmc-key") || strings.Contains(string(b), "test-cg-key") {
		t.Fatal("status leaked credentials")
	}
}
func TestVerifiedOutageHasBoundedCacheLifetime(t *testing.T) {
	f := setupVerified(t)
	if GetRateForCoin("usdt", "cny") == 0 {
		t.Fatal("initial quote")
	}
	expiry := f.stored.ExpiresAt
	f.now = f.now.Add(301 * time.Second)
	f.primaryStatus = 503
	f.checkStatus = 429
	if GetRateForCoin("usdt", "cny") != 1.0/7 {
		t.Fatal("temporary outage should use verified quote")
	}
	if f.stored.Paused || !f.stored.ExpiresAt.Equal(expiry) {
		t.Fatal("outage paused valid cache or extended expiry")
	}
	f.now = expiry
	if GetRateForCoin("usdt", "cny") != 0 {
		t.Fatal("expired quote accepted")
	}
	ResetRateCacheRuntime()
	if GetRateForCoin("usdt", "cny") != 0 {
		t.Fatal("restart revived expiry")
	}
	if f.alerts.Load() != 1 {
		t.Fatalf("alerts not throttled: %d", f.alerts.Load())
	}
}
func TestVerifiedColdOutageAndUnsupportedPairsDoNotFallback(t *testing.T) {
	f := setupVerified(t)
	f.primaryStatus = 503
	for _, pair := range [][2]string{{"usdt", "cny"}, {"usdt", "usd"}, {"eth", "cny"}} {
		if GetRateForCoin(pair[0], pair[1]) != 0 {
			t.Fatal("cold outage or unsupported pair returned quote")
		}
	}
	f.cfg.Currency = "usd"
	f.cfg.MinPrice = .9
	f.cfg.MaxPrice = 1.1
	f.cfg.ReferencePrice = 1
	if GetRateForCoin("usdt", "usd") != 0 {
		t.Fatal("configured USD outage silently used 1:1")
	}
}
func TestVerifiedAnomaliesPauseEvenFreshCache(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*verifiedFixture)
	}{
		{"conflict", func(f *verifiedFixture) { f.check = 7.1 }},
		{"both jump", func(f *verifiedFixture) { f.primary = 8; f.check = 8 }},
		{"tenfold", func(f *verifiedFixture) { f.primary = 70; f.check = 70 }},
		{"zero", func(f *verifiedFixture) { f.primary = 0 }},
		{"negative", func(f *verifiedFixture) { f.check = -7 }},
		{"stale", func(f *verifiedFixture) { f.quoteTime = f.now.Add(-601 * time.Second) }},
		{"future", func(f *verifiedFixture) { f.quoteTime = f.now.Add(time.Second) }},
		{"schema", func(f *verifiedFixture) { f.primaryBody = `{"status":{"error_code":0},"data":{"825":{"price":7}}}` }},
		{"wrong asset", func(f *verifiedFixture) {
			f.primaryBody = `{"status":{"error_code":0},"data":[{"id":1,"symbol":"BTC","quote":[]}]}`
		}},
		{"missing timestamp", func(f *verifiedFixture) { f.checkBody = `{"tether":{"cny":7}}` }},
		{"auth", func(f *verifiedFixture) { f.checkStatus = 401 }},
		{"bad quote plus outage", func(f *verifiedFixture) { f.primary = 70; f.checkStatus = 503 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setupVerified(t)
			if GetRateForCoin("usdt", "cny") == 0 {
				t.Fatal("initial quote")
			}
			accepted := f.stored.Primary
			test.mutate(f)
			result := RefreshVerifiedRate()
			if result.Usable || !result.State.Paused {
				t.Fatalf("anomaly did not pause: %+v", result.State)
			}
			if f.stored.Primary != accepted {
				t.Fatal("anomaly overwrote accepted quote")
			}
			if GetRateForCoin("usdt", "cny") != 0 {
				t.Fatal("paused quote accepted")
			}
			ResetRateCacheRuntime()
			if GetRateForCoin("usdt", "cny") != 0 {
				t.Fatal("restart revived pause")
			}
		})
	}
}
func TestVerifiedRecoveryDoesNotRenewSourceAge(t *testing.T) {
	f := setupVerified(t)
	GetRateForCoin("usdt", "cny")
	expiry := f.stored.ExpiresAt
	f.check = 7.1
	RefreshVerifiedRate()
	f.check = 7.01
	f.now = f.now.Add(61 * time.Second)
	if GetRateForCoin("usdt", "cny") == 0 {
		t.Fatal("valid pair did not recover")
	}
	if !f.stored.ExpiresAt.Equal(expiry) {
		t.Fatal("same quote received a renewed lifetime")
	}
}
func TestVerifiedPolicyChangesAndWriteFailuresFailClosed(t *testing.T) {
	f := setupVerified(t)
	GetRateForCoin("usdt", "cny")
	f.failSave = true
	f.now = f.now.Add(301 * time.Second)
	f.quoteTime = f.now
	if GetRateForCoin("usdt", "cny") != 0 || GetRateForCoin("usdt", "cny") != 0 {
		t.Fatal("unpersisted or old quote accepted after write failure")
	}
	f.failSave = false
	f.cfg.MaxSpreadPercent = .5
	f.primaryStatus = 503
	if GetRateForCoin("usdt", "cny") != 0 {
		t.Fatal("changed policy reused old quote")
	}
}
func TestVerifiedTimestampSkewAndBackwardCheck(t *testing.T) {
	f := setupVerified(t)
	GetRateForCoin("usdt", "cny")
	p, q := f.stored.Primary, f.stored.Check
	q.QuotedAt = p.QuotedAt.Add(-181 * time.Second)
	if validateVerifiedQuotes(p, q, VerifiedRateState{}, f.cfg, f.now) == nil {
		t.Fatal("time skew accepted")
	}
	q = f.stored.Check
	p.QuotedAt = p.QuotedAt.Add(-time.Second)
	if validateVerifiedQuotes(p, q, f.stored, f.cfg, f.now) == nil {
		t.Fatal("backward timestamp accepted")
	}
}
func TestVerifiedRejectsInvalidPolicies(t *testing.T) {
	f := setupVerified(t)
	for _, change := range []func(*VerifiedRateConfig){func(c *VerifiedRateConfig) { c.CMCAPIKey = "" }, func(c *VerifiedRateConfig) { c.MaxAgeSeconds = 99999 }, func(c *VerifiedRateConfig) { c.MaxSpreadPercent = 0 }, func(c *VerifiedRateConfig) { c.ReferencePrice = 0 }, func(c *VerifiedRateConfig) { c.MinPrice = 10 }, func(c *VerifiedRateConfig) { c.CoinGeckoAPIKey = "header\ninjection" }} {
		c := f.cfg
		change(&c)
		if ValidateVerifiedRateConfig(c) == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	if _, e := ParseVerifiedRateConfig(`{"unknown":true}`); e == nil {
		t.Fatal("unknown fields accepted")
	}
}

func TestVerifiedConfigChangeDiscardsInflightResponse(t *testing.T) {
	f := setupVerified(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-release
		body := fmt.Sprintf(`{"status":{"error_code":0},"data":[{"id":825,"symbol":"USDT","quote":[{"symbol":"CNY","price":7,"last_updated":%q}]}]}`, f.now.Format(time.RFC3339))
		if r.URL.Host == "pro-api.coingecko.com" {
			body = fmt.Sprintf(`{"tether":{"cny":7,"last_updated_at":%d}}`, f.now.Unix())
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	done := make(chan float64, 1)
	go func() { done <- GetRateForCoin("usdt", "cny") }()
	<-entered
	<-entered
	f.mu.Lock()
	f.cfg.MaxSpreadPercent = .5
	f.mu.Unlock()
	close(release)
	if <-done != 0 || f.hasState {
		t.Fatal("old in-flight response published after policy change")
	}
}

func TestVerifiedRejectsRedirectWithoutLeakingKeys(t *testing.T) {
	f := setupVerified(t)
	var requests atomic.Int32
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.Host == "attacker.example" {
			t.Error("followed redirect with credentials")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://attacker.example/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if GetRateForCoin("usdt", "cny") != 0 || !f.stored.Paused || requests.Load() != 2 {
		t.Fatal("redirect was not rejected")
	}
}

func TestVerifiedUSDUsesBothProvidersAndLegacyRefreshSelectsConfiguredBase(t *testing.T) {
	f := setupVerified(t)
	f.cfg.Currency = "usd"
	f.cfg.ReferencePrice = 1
	f.cfg.MinPrice = .9
	f.cfg.MaxPrice = 1.1
	f.primary = .999
	f.check = 1
	bases := KnownRateBases()
	if len(bases) != 1 || bases[0] != "usd" {
		t.Fatalf("refresh bases=%v", bases)
	}
	if result := RefreshRateBase("usd", true); !result.OK {
		t.Fatalf("refresh=%+v", result)
	}
	if rate := GetRateForCoin("usdt", "usd"); rate != 1/.999 || f.calls.Load() != 2 {
		t.Fatal("USD quote bypassed verified providers")
	}
}
