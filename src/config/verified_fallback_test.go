package config

import (
	"strings"
	"testing"
	"time"
)

func TestPerOrderFallbackRetriesAndReturnsToAutomatic(t *testing.T) {
	f := setupVerified(t)
	f.cfg.FallbackEnabled = true
	f.cfg.FallbackPrice = 6.8
	f.check = 7.1
	rate, audit := GetPaymentRate("usdt", "cny")
	if rate != 1/6.8 || !strings.Contains(audit, `"fixed_fallback"`) || !strings.Contains(audit, "provider prices disagree") {
		t.Fatal("missing fixed fallback decision", rate, audit)
	}
	if !GetVerifiedRateStatus().FallbackAvailable {
		t.Fatal("status omitted available fallback")
	}
	// No clock advance: the NEXT order must try again, even during old retry TTL.
	f.check = 7.01
	rate, audit = GetPaymentRate("usdt", "cny")
	if rate != 1.0/7 || !strings.Contains(audit, `"auto_verified"`) || f.calls.Load() != 4 {
		t.Fatal("fallback stuck after recovery", rate, audit, f.calls.Load())
	}
	if GetRateMode() != RateModeAuto {
		t.Fatal("fallback mutated global mode")
	}
	// A temporary outage can still reuse a previously verified, unexpired quote.
	f.primaryStatus = 503
	rate, audit = GetPaymentRate("usdt", "cny")
	if rate != 1.0/7 || !strings.Contains(audit, `"auto_verified"`) {
		t.Fatal("trusted cache skipped")
	}
	f.now = f.now.Add(601 * time.Second)
	rate, audit = GetPaymentRate("usdt", "cny")
	if rate != 1/6.8 || !strings.Contains(audit, `"fixed_fallback"`) {
		t.Fatal("expired auto quote did not fall back")
	}
}

func TestFixedFallbackExpiryStorageAndPairBoundaries(t *testing.T) {
	f := setupVerified(t)
	f.cfg.FallbackEnabled = true
	f.cfg.FallbackPrice = 7
	f.primaryStatus = 503
	f.cfg.FallbackExpiresAt = f.now.Add(time.Minute).Format(time.RFC3339)
	if rate, _ := GetPaymentRate("usdt", "cny"); rate == 0 {
		t.Fatal("cold unavailable should fall back")
	}
	for _, pair := range [][2]string{{"usdt", "usd"}, {"eth", "cny"}} {
		if rate, _ := GetPaymentRate(pair[0], pair[1]); rate != 0 {
			t.Fatal("unsupported pair fell back")
		}
	}
	f.now = f.now.Add(time.Minute)
	if rate, _ := GetPaymentRate("usdt", "cny"); rate != 0 {
		t.Fatal("expired fallback accepted")
	}
	f.cfg.FallbackExpiresAt = ""
	f.failSave = true
	if rate, _ := GetPaymentRate("usdt", "cny"); rate != 0 {
		t.Fatal("storage failure bypassed by fallback")
	}
	f.failSave = false
	f.cfg.FallbackEnabled = false
	if rate, _ := GetPaymentRate("usdt", "cny"); rate != 0 {
		t.Fatal("disabled fallback accepted")
	}
}

func TestFallbackConfigurationValidation(t *testing.T) {
	f := setupVerified(t)
	f.cfg.FallbackEnabled = true
	for _, p := range []float64{0, -1, 1e-320, 70} {
		f.cfg.FallbackPrice = p
		if ValidateVerifiedRateConfig(f.cfg) == nil {
			t.Fatal("invalid fallback price accepted", p)
		}
	}
	f.cfg.FallbackPrice = 7
	f.cfg.FallbackExpiresAt = "tomorrow"
	if ValidateVerifiedRateConfig(f.cfg) == nil {
		t.Fatal("invalid expiry accepted")
	}
}
