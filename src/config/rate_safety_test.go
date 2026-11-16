package config

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func installRateClock(t *testing.T, now time.Time) {
	t.Helper()
	previous := rateNow
	rateNow = func() time.Time { return now }
	t.Cleanup(func() {
		rateAsyncRefreshes.Wait()
		rateNow = previous
	})
}

func rateResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK",
		Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestRateSourceDefaultsAndConfiguredPrecedence(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("API_RATE_URL", "")
	values := map[string]string{}
	installSettingsGetter(t, values)
	if got := GetRateApiUrl(); got != DefaultRateAPIURL {
		t.Fatalf("empty configuration = %q", got)
	}
	if got := GetRateApiUrlFromEnv(); got != "" {
		t.Fatalf("default must not be seeded as explicit environment setting: %q", got)
	}
	t.Setenv("API_RATE_URL", "https://env.example.test/")
	if got := GetRateApiUrl(); got != "https://env.example.test" {
		t.Fatalf("environment source = %q", got)
	}
	viper.Set("api_rate_url", "https://file.example.test/")
	if got := GetRateApiUrl(); got != "https://file.example.test" {
		t.Fatalf("file source = %q", got)
	}
	values["rate.api_url"] = " https://admin.example.test/// "
	if got := GetRateApiUrl(); got != "https://admin.example.test" {
		t.Fatalf("admin source = %q", got)
	}
	values["rate.api_url"] = " "
	if got := GetRateApiUrl(); got != "https://file.example.test" {
		t.Fatalf("blank admin override = %q", got)
	}
}

func TestRateQuoteDatesAndCacheValidity(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, source, body string
		valid              bool
		expires            time.Time
	}{
		{"default yesterday", DefaultRateAPIURL, `{"date":"2026-11-05","cny":{"usdt":0.125}}`, true, time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)},
		{"custom today", "https://custom.test", `{"date":"2026-11-06","cny":{"usdt":0.125}}`, true, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)},
		{"custom no date", "https://custom.test", `{"cny":{"usdt":0.125}}`, true, now.Add(MaxRateQuoteAge)},
		{"default no date", DefaultRateAPIURL, `{"cny":{"usdt":0.125}}`, false, time.Time{}},
		{"old date", "https://custom.test", `{"date":"2026-11-04","cny":{"usdt":0.125}}`, false, time.Time{}},
		{"future date", "https://custom.test", `{"date":"2026-11-07","cny":{"usdt":0.125}}`, false, time.Time{}},
		{"malformed date", "https://custom.test", `{"date":"06/11/2026","cny":{"usdt":0.125}}`, false, time.Time{}},
		{"null date", "https://custom.test", `{"date":null,"cny":{"usdt":0.125}}`, false, time.Time{}},
		{"numeric date", "https://custom.test", `{"date":123,"cny":{"usdt":0.125}}`, false, time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installRateClock(t, now)
			installSettingsGetter(t, map[string]string{"rate.mode": "auto", "rate.api_url": tc.source})
			installRateCacheStore(t)
			installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != tc.source+"/cny.json" && !(tc.source == DefaultRateAPIURL && r.URL.String() == DefaultRateFallbackURL+"/cny.json") {
					t.Errorf("request URL = %s", r.URL)
				}
				return rateResponse(r, tc.body), nil
			})
			rate := GetRateForCoin("usdt", "cny")
			if (rate == 0.125) != tc.valid || (!tc.valid && rate != 0) {
				t.Fatalf("rate=%v, valid=%v", rate, tc.valid)
			}
			status := GetRateStatus().Bases[0]
			if status.IsUsable != tc.valid || status.LastRefreshOK != tc.valid {
				t.Fatalf("status=%+v", status)
			}
			if tc.valid && (status.ExpiresAt == nil || !status.ExpiresAt.Equal(tc.expires)) {
				t.Fatalf("expiry=%v, want %v", status.ExpiresAt, tc.expires)
			}
		})
	}
}

func TestExpiredOrLegacyCacheRequiresRefreshAndNeverUsesFixedFallback(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, legacy := range []bool{false, true} {
		for _, available := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{true: "legacy", false: "expired"}[legacy], map[bool]string{true: "success", false: "failure"}[available]}, "/"), func(t *testing.T) {
				installRateClock(t, now)
				installSettingsGetter(t, map[string]string{
					"rate.mode": "auto", "rate.api_url": "https://custom.test",
					"rate.forced_rate_list": `{"cny":{"usdt":0.999}}`,
				})
				expiresAt := timePointer(now) // exact boundary must be rejected
				if legacy {
					expiresAt = nil
				}
				store := installRateCacheStore(t, RateCacheSnapshot{
					Base: "cny", SourceURL: "https://custom.test", Rates: map[string]float64{"usdt": 0.14},
					LastSuccessAt: timePointer(now.Add(-time.Second)), LastAttemptAt: timePointer(now.Add(-time.Second)),
					LastRefreshOK: true, ExpiresAt: expiresAt,
				})
				var calls atomic.Int32
				installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					if !available {
						return nil, errors.New("offline")
					}
					return rateResponse(r, `{"cny":{"usdt":0.15}}`), nil
				})
				want := 0.0
				if available {
					want = 0.15
				}
				for i := 0; i < 2; i++ {
					if got := GetRateForCoin("usdt", "cny"); got != want {
						t.Fatalf("rate=%v, want %v", got, want)
					}
					ResetRateCacheRuntime()
				}
				if calls.Load() != 1 {
					t.Fatalf("requests=%d, want one refresh/retry throttle", calls.Load())
				}
				store.mu.Lock()
				row := cloneSnapshot(store.rows["cny"])
				store.mu.Unlock()
				if !available && row.ExpiresAt != nil && !row.ExpiresAt.Equal(now) {
					t.Fatalf("failure extended validity: %+v", row)
				}
			})
		}
	}
}

func TestRefetchingSameQuoteDateDoesNotExtendExpiry(t *testing.T) {
	now := time.Date(2026, 11, 6, 23, 59, 0, 0, time.UTC)
	installRateClock(t, now)
	installSettingsGetter(t, map[string]string{"rate.mode": "auto", "rate.api_url": "https://custom.test"})
	installRateCacheStore(t)
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return rateResponse(r, `{"date":"2026-11-05","cny":{"usdt":0.125}}`), nil
	})
	first := RefreshRateBase("cny", true)
	second := RefreshRateBase("cny", true)
	if !first.OK || !second.OK || first.ExpiresAt == nil || !first.ExpiresAt.Equal(*second.ExpiresAt) || !second.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("same-date quotes extended expiry: %+v / %+v", first, second)
	}
	// The payload is now exactly 48h old, even though it was fetched a minute ago.
	rateNow = func() time.Time { return now.Add(time.Minute) }
	if got := GetRateForCoin("usdt", "cny"); got != 0 {
		t.Fatalf("expired quote=%v", got)
	}
}

func installMutableRateSource(t *testing.T, initial string) func(string) {
	t.Helper()
	installSettingsGetter(t, map[string]string{"rate.mode": "auto"})
	var source atomic.Value
	source.Store(initial)
	SettingsGetString = func(key string) string {
		if key == "rate.mode" {
			return RateModeAuto
		}
		if key == "rate.api_url" {
			return source.Load().(string)
		}
		return ""
	}
	return func(value string) { source.Store(value) }
}

func TestSwitchingSourceDoesNotUsePreviousQuoteOrRetryDelay(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(map[bool]string{true: "new source online", false: "new source offline"}[available], func(t *testing.T) {
			now := time.Now().UTC()
			installRateClock(t, now)
			setSource := installMutableRateSource(t, "https://old.test/")
			installRateCacheStore(t, RateCacheSnapshot{
				Base: "cny", SourceURL: "https://old.test", Rates: map[string]float64{"usdt": 0.14},
				LastSuccessAt: &now, LastAttemptAt: &now, LastRefreshOK: true, ExpiresAt: timePointer(now.Add(time.Hour)),
			})
			var calls atomic.Int32
			installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.URL.Host != "new.test" {
					t.Errorf("unexpected source %s", r.URL)
				}
				if !available {
					return nil, errors.New("offline")
				}
				return rateResponse(r, `{"cny":{"usdt":0.15}}`), nil
			})
			if got := GetRateForCoin("usdt", "cny"); got != 0.14 {
				t.Fatalf("normalized old source=%v", got)
			}
			setSource("https://new.test")
			want := 0.0
			if available {
				want = 0.15
			}
			for i := 0; i < 2; i++ {
				if got := GetRateForCoin("usdt", "cny"); got != want {
					t.Fatalf("new source quote=%v, want %v", got, want)
				}
				ResetRateCacheRuntime()
			}
			if calls.Load() != 1 {
				t.Fatalf("requests=%d", calls.Load())
			}
			row, _ := loadRateCacheSnapshot("cny")
			if row.SourceURL != "https://new.test" || (!available && len(row.Rates) != 0) {
				t.Fatalf("previous provider quote relabeled: %+v", row)
			}
		})
	}
}

func TestInFlightOldSourceCannotOverwriteNewSource(t *testing.T) {
	setSource := installMutableRateSource(t, "https://old.test")
	installRateCacheStore(t)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan RateRefreshResult, 1)
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "old.test" {
			close(started)
			<-release
			return rateResponse(r, `{"cny":{"usdt":0.14}}`), nil
		}
		return rateResponse(r, `{"cny":{"usdt":0.15}}`), nil
	})
	go func() { done <- RefreshRateBase("cny", true) }()
	<-started
	setSource("https://new.test")
	result := RefreshRateBase("cny", true)
	close(release)
	old := <-done
	if !result.OK || old.OK || !strings.Contains(old.Error, "source changed") {
		t.Fatalf("new=%+v old=%+v", result, old)
	}
	ResetRateCacheRuntime()
	if got := GetRateForCoin("usdt", "cny"); got != 0.15 {
		t.Fatalf("new source overwritten: %v", got)
	}
}
