package config

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/util/http_client"
	"github.com/go-resty/resty/v2"
)

func TestDefaultRateEndpointFallbackKeepsValidatedSourceIdentity(t *testing.T) {
	for _, primary := range []string{"valid", "network", "http", "redirect", "json", "expired", "future", "missing date", "missing coin", "negative coin", "oversize"} {
		t.Run(primary, func(t *testing.T) {
			now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
			installRateClock(t, now)
			installSettingsGetter(t, map[string]string{"rate.mode": "auto", "rate.api_url": DefaultRateAPIURL})
			installRateCacheStore(t)
			var primaryCalls, fallbackCalls atomic.Int32
			installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				if !strings.HasPrefix(r.UserAgent(), "USDT-QX/") {
					t.Errorf("official endpoint missing application identity: %q", r.UserAgent())
				}
				if r.URL.String() == DefaultRateFallbackURL+"/cny.json" {
					fallbackCalls.Add(1)
					return rateResponse(r, `{"date":"2026-11-16","cny":{"usdt":0.125}}`), nil
				}
				if r.URL.String() != DefaultRateAPIURL+"/cny.json" {
					t.Errorf("request escaped fixed official endpoints: %s", r.URL)
					return nil, errors.New("unexpected endpoint")
				}
				primaryCalls.Add(1)
				switch primary {
				case "network":
					return nil, errors.New("offline")
				case "http":
					resp := rateResponse(r, `{}`)
					resp.StatusCode, resp.Status = http.StatusServiceUnavailable, "503 Service Unavailable"
					return resp, nil
				case "redirect":
					resp := rateResponse(r, `{}`)
					resp.StatusCode, resp.Status = http.StatusFound, "302 Found"
					resp.Header.Set("Location", "https://untrusted.test/cny.json")
					return resp, nil
				case "json":
					return rateResponse(r, `<html>unavailable</html>`), nil
				case "expired":
					return rateResponse(r, `{"date":"2026-11-14","cny":{"usdt":0.9}}`), nil
				case "future":
					return rateResponse(r, `{"date":"2026-11-17","cny":{"usdt":0.9}}`), nil
				case "missing date":
					return rateResponse(r, `{"cny":{"usdt":0.9}}`), nil
				case "missing coin":
					return rateResponse(r, `{"date":"2026-11-16","cny":{"usdc":0.9}}`), nil
				case "negative coin":
					return rateResponse(r, `{"date":"2026-11-16","cny":{"usdt":-0.9}}`), nil
				case "oversize":
					return rateResponse(r, strings.Repeat(" ", (1<<20)+1)), nil
				default:
					return rateResponse(r, `{"date":"2026-11-16","cny":{"usdt":0.14}}`), nil
				}
			})
			// Even a caller-supplied client retry policy must not multiply the
			// bounded primary/fallback attempts in this built-in path.
			factory := http_client.ClientFactory
			http_client.ClientFactory = func() *resty.Client { return factory().SetRetryCount(3) }
			want := 0.125
			if primary == "valid" {
				want = 0.14
			}
			if got := GetRateForCoin("usdt", "cny"); got != want {
				t.Fatalf("rate=%v, want %v", got, want)
			}
			ResetRateCacheRuntime()
			if got := GetRateForCoin("usdt", "cny"); got != want {
				t.Fatalf("restored rate=%v, want %v", got, want)
			}
			status := GetRateStatus().Bases[0]
			if status.SourceURL != DefaultRateAPIURL || !status.IsUsable || status.QuoteDate != "2026-11-16" || status.ExpiresAt == nil || !status.ExpiresAt.Equal(time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("fallback changed cache identity or validity: %+v", status)
			}
			wantFallback := int32(1)
			if primary == "valid" {
				wantFallback = 0
			}
			if primaryCalls.Load() != 1 || fallbackCalls.Load() != wantFallback {
				t.Fatalf("unexpected retries or cache miss: primary=%d fallback=%d", primaryCalls.Load(), fallbackCalls.Load())
			}
		})
	}
}

func TestInvalidDefaultFallbackCannotRenewExpiredQuote(t *testing.T) {
	for _, body := range []string{`{"cny":{"usdt":0.125}}`, `{"date":"2026-11-14","cny":{"usdt":0.125}}`, `{"date":"2026-11-17","cny":{"usdt":0.125}}`, `{"date":"2026-11-16","cny":{"usdc":0.125}}`, `{"date":"2026-11-16","cny":{"usdt":0}}`} {
		t.Run(body, func(t *testing.T) {
			now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
			installRateClock(t, now)
			installSettingsGetter(t, map[string]string{"rate.mode": "auto", "rate.api_url": DefaultRateAPIURL, "rate.forced_rate_list": `{"cny":{"usdt":0.999}}`})
			installRateCacheStore(t, RateCacheSnapshot{Base: "cny", SourceURL: DefaultRateAPIURL, Rates: map[string]float64{"usdt": 0.14}, LastSuccessAt: timePointer(now.Add(-time.Hour)), LastAttemptAt: timePointer(now.Add(-time.Hour)), LastRefreshOK: true, ExpiresAt: &now})
			installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == DefaultRateAPIURL+"/cny.json" {
					return nil, errors.New("offline")
				}
				return rateResponse(r, body), nil
			})
			if got := GetRateForCoin("usdt", "cny"); got != 0 {
				t.Fatalf("invalid fallback yielded payable rate %v", got)
			}
			row, _ := loadRateCacheSnapshot("cny")
			if row.LastRefreshOK || row.ExpiresAt == nil || !row.ExpiresAt.Equal(now) || !strings.Contains(row.LastError, "jsDelivr:") || !strings.Contains(row.LastError, "Cloudflare:") {
				t.Fatalf("invalid fallback renewed or hid failure: %+v", row)
			}
		})
	}
}

func TestCustomRateEndpointNeverUsesBuiltInFallback(t *testing.T) {
	installSettingsGetter(t, map[string]string{"rate.mode": "auto", "rate.api_url": "https://custom.test"})
	installRateCacheStore(t)
	var calls atomic.Int32
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "custom.test" {
			t.Errorf("custom source escaped to %s", r.URL)
		}
		return nil, errors.New("offline")
	})
	if got := GetRateForCoin("usdt", "cny"); got != 0 || calls.Load() != 1 {
		t.Fatalf("custom source fallback: rate=%v calls=%d", got, calls.Load())
	}
}

func TestInFlightDefaultFallbackCannotOverwriteChangedSource(t *testing.T) {
	installRateClock(t, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	setSource := installMutableRateSource(t, DefaultRateAPIURL)
	installRateCacheStore(t)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan RateRefreshResult, 1)
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "cdn.jsdelivr.net":
			return nil, errors.New("offline")
		case "latest.currency-api.pages.dev":
			close(started)
			<-release
			return rateResponse(r, `{"date":"2026-11-16","cny":{"usdt":0.125}}`), nil
		default:
			return rateResponse(r, `{"date":"2026-11-16","cny":{"usdt":0.15}}`), nil
		}
	})
	go func() { done <- RefreshRateBase("cny", true) }()
	<-started
	setSource("https://new.test")
	current := RefreshRateBase("cny", true)
	close(release)
	old := <-done
	if !current.OK || old.OK || !strings.Contains(old.Error, "source changed") {
		t.Fatalf("current=%+v old=%+v", current, old)
	}
	ResetRateCacheRuntime()
	if got := GetRateForCoin("usdt", "cny"); got != 0.15 {
		t.Fatalf("late fallback overwrote current source: %v", got)
	}
}

func TestDefaultRateTimeoutLeavesTimeForFallbackAndBoundsTotal(t *testing.T) {
	for _, fallbackAvailable := range []bool{true, false} {
		t.Run(map[bool]string{true: "primary stalled", false: "both stalled"}[fallbackAvailable], func(t *testing.T) {
			installRateClock(t, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
			installSettingsGetter(t, map[string]string{"rate.mode": "auto", "rate.api_url": DefaultRateAPIURL})
			installRateCacheStore(t)
			installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				if fallbackAvailable && r.URL.String() == DefaultRateFallbackURL+"/cny.json" {
					return rateResponse(r, `{"date":"2026-11-16","cny":{"usdt":0.125}}`), nil
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			start := time.Now()
			rate := GetRateForCoin("usdt", "cny")
			if elapsed := time.Since(start); elapsed > defaultRateFetchTimeout+time.Second {
				t.Fatalf("refresh exceeded total deadline: %s", elapsed)
			}
			want := 0.0
			if fallbackAvailable {
				want = 0.125
			}
			if rate != want {
				t.Fatalf("rate=%v, want %v", rate, want)
			}
		})
	}
}
