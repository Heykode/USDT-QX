package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

const VerifiedRateSettingKey = "rate.verified_usdt"

// All fields live in one setting so a policy and its credentials change atomically.
// This setting is never included in the generic settings-list response.
type VerifiedRateConfig struct {
	Enabled            bool    `json:"enabled"`
	Currency           string  `json:"currency"`
	CMCAPIKey          string  `json:"cmc_api_key,omitempty"`
	CoinGeckoAPIKey    string  `json:"coingecko_api_key,omitempty"`
	CoinGeckoPlan      string  `json:"coingecko_plan"`
	MinPrice           float64 `json:"min_price"`
	MaxPrice           float64 `json:"max_price"`
	ReferencePrice     float64 `json:"reference_price"`
	MaxSpreadPercent   float64 `json:"max_spread_percent"`
	MaxJumpPercent     float64 `json:"max_jump_percent"`
	RefreshSeconds     int     `json:"refresh_seconds"`
	MaxAgeSeconds      int     `json:"max_age_seconds"`
	MaxTimeSkewSeconds int     `json:"max_time_skew_seconds"`
	FallbackEnabled    bool    `json:"fallback_enabled"`
	FallbackPrice      float64 `json:"fallback_price"`      // billing currency per USDT
	FallbackExpiresAt  string  `json:"fallback_expires_at"` // optional RFC3339 deadline
}

func DefaultVerifiedRateConfig() VerifiedRateConfig {
	return VerifiedRateConfig{Currency: "cny", CoinGeckoPlan: "pro", MaxSpreadPercent: 1, MaxJumpPercent: 2, RefreshSeconds: 300, MaxAgeSeconds: 600, MaxTimeSkewSeconds: 180}
}

func ParseVerifiedRateConfig(raw string) (VerifiedRateConfig, error) {
	cfg := DefaultVerifiedRateConfig()
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if strings.TrimSpace(raw) == "null" {
		return cfg, fmt.Errorf("invalid verified rate configuration")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("invalid verified rate configuration")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return cfg, fmt.Errorf("invalid verified rate configuration")
	}
	return cfg, ValidateVerifiedRateConfig(cfg)
}

func ValidateVerifiedRateConfig(c VerifiedRateConfig) error {
	if c.FallbackExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, c.FallbackExpiresAt); err != nil {
			return fmt.Errorf("invalid fixed fallback expiry")
		}
	}
	if c.FallbackEnabled && (!positiveFinite(c.FallbackPrice) || !positiveFinite(1/c.FallbackPrice) || c.FallbackPrice < c.MinPrice || c.FallbackPrice > c.MaxPrice) {
		return fmt.Errorf("fixed fallback price must be positive and within administrator limits")
	}
	if c.Currency != "cny" && c.Currency != "usd" {
		return fmt.Errorf("currency must be cny or usd")
	}
	if c.CoinGeckoPlan != "pro" && c.CoinGeckoPlan != "demo" {
		return fmt.Errorf("CoinGecko plan must be pro or demo")
	}
	for _, key := range []string{c.CMCAPIKey, c.CoinGeckoAPIKey} {
		if len(key) > 256 || strings.ContainsAny(key, "\r\n\t ") {
			return fmt.Errorf("invalid API key format")
		}
	}
	if !positiveFinite(c.MaxSpreadPercent) || c.MaxSpreadPercent > 5 || !positiveFinite(c.MaxJumpPercent) || c.MaxJumpPercent > 10 {
		return fmt.Errorf("spread must be >0 and <=5 percent; jump must be >0 and <=10 percent")
	}
	if c.RefreshSeconds < 60 || c.RefreshSeconds > 600 || c.MaxAgeSeconds < c.RefreshSeconds || c.MaxAgeSeconds > 1800 || c.MaxTimeSkewSeconds < 1 || c.MaxTimeSkewSeconds > c.MaxAgeSeconds {
		return fmt.Errorf("invalid refresh, maximum age or time skew (seconds)")
	}
	if !c.Enabled {
		return nil
	}
	if c.CMCAPIKey == "" || c.CoinGeckoAPIKey == "" {
		return fmt.Errorf("both provider API keys are required")
	}
	if !positiveFinite(c.MinPrice) || !positiveFinite(c.MaxPrice) || !positiveFinite(c.ReferencePrice) || c.MinPrice >= c.MaxPrice || c.ReferencePrice < c.MinPrice || c.ReferencePrice > c.MaxPrice {
		return fmt.Errorf("set a positive price range and a reference within that range")
	}
	return nil
}

func GetVerifiedRateConfig() (VerifiedRateConfig, error) {
	raw := ""
	if SettingsGetString != nil {
		raw = SettingsGetString(VerifiedRateSettingKey)
	}
	return ParseVerifiedRateConfig(raw)
}

func verifiedConfigID(c VerifiedRateConfig) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type ProviderQuote struct {
	Provider string    `json:"provider"`
	Currency string    `json:"currency"`
	Price    float64   `json:"price"` // base currency per USDT, never the reciprocal
	QuotedAt time.Time `json:"quoted_at"`
}

type VerifiedRateState struct {
	Base            string        `json:"base"`
	ConfigID        string        `json:"config_id"`
	Primary         ProviderQuote `json:"primary"` // last ACCEPTED quotes; never overwritten by bad data
	Check           ProviderQuote `json:"check"`
	ObservedPrimary ProviderQuote `json:"observed_primary"`
	ObservedCheck   ProviderQuote `json:"observed_check"`
	SpreadPercent   float64       `json:"spread_percent"`
	AcceptedAt      time.Time     `json:"accepted_at"`
	ExpiresAt       time.Time     `json:"expires_at"`
	LastAttemptAt   time.Time     `json:"last_attempt_at"`
	Paused          bool          `json:"paused"`
	LastError       string        `json:"last_error"`
	FailureKind     string        `json:"failure_kind"`
	LastAlertAt     time.Time     `json:"last_alert_at"`
}

var (
	VerifiedRateLoad   func(base string) (VerifiedRateState, bool, error)
	VerifiedRateSave   func(VerifiedRateState) error
	VerifiedRateAlert  func(string)
	verifiedRateMu     sync.Mutex
	verifiedRateMemory = map[string]VerifiedRateState{}
)

func resetVerifiedRateMemory() {
	verifiedRateMu.Lock()
	defer verifiedRateMu.Unlock()
	verifiedRateMemory = map[string]VerifiedRateState{}
}

// GetPaymentRate returns the conversion rate and the exact audit snapshot used
// by this call. Callers must store that snapshot with the newly created order.
func GetPaymentRate(coin, base string) (float64, string) {
	coin, base = normalizeRateKey(coin), normalizeRateKey(base)
	mode := GetRateMode()
	if mode == RateModeAuto {
		cfg, err := GetVerifiedRateConfig()
		if err != nil {
			return 0, ""
		}
		if cfg.Enabled {
			return verifiedPaymentRate(coin, base, cfg)
		}
	}
	rate := getLegacyRateForCoin(coin, base, mode)
	if !positiveFinite(rate) {
		return 0, ""
	}
	raw, _ := json.Marshal(map[string]any{"mode": mode, "coin": coin, "base": base, "rate": rate})
	return rate, string(raw)
}

func verifiedPaymentRate(coin, base string, cfg VerifiedRateConfig) (float64, string) {
	if coin != "usdt" || base != cfg.Currency {
		return 0, ""
	} // never fall back to unverified pairs (including USD 1:1)
	// With per-order fallback enabled, each request attempts the providers first.
	// Only concurrent calls may share a check completed after they started.
	state, err := refreshVerifiedRate(cfg, cfg.FallbackEnabled)
	if err != nil || GetRateMode() != RateModeAuto {
		return 0, ""
	}
	if !verifiedStateUsable(state, cfg, rateNow()) {
		return fixedFallbackRate(state, cfg, rateNow())
	}
	raw, _ := json.Marshal(struct {
		Mode  string            `json:"mode"`
		Coin  string            `json:"coin"`
		Rate  float64           `json:"rate"`
		State VerifiedRateState `json:"verification"`
	}{"auto_verified", "usdt", 1 / state.Primary.Price, state})
	return 1 / state.Primary.Price, string(raw)
}

func fallbackUsable(s VerifiedRateState, cfg VerifiedRateConfig, now time.Time) bool {
	current, err := GetVerifiedRateConfig()
	if err != nil || !current.Enabled || !cfg.FallbackEnabled || GetRateMode() != RateModeAuto ||
		verifiedConfigID(current) != verifiedConfigID(cfg) || s.ConfigID != verifiedConfigID(cfg) ||
		s.LastAttemptAt.IsZero() || s.FailureKind == "storage" ||
		!positiveFinite(cfg.FallbackPrice) || !positiveFinite(1/cfg.FallbackPrice) {
		return false
	}
	if cfg.FallbackExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, cfg.FallbackExpiresAt)
		if err != nil || !now.Before(expiry) {
			return false
		}
	}
	return true
}

func fixedFallbackRate(s VerifiedRateState, cfg VerifiedRateConfig, now time.Time) (float64, string) {
	if !fallbackUsable(s, cfg, now) {
		return 0, ""
	}
	reason := s.LastError
	if reason == "" {
		reason = "no usable verified quote"
	}
	raw, err := json.Marshal(map[string]any{
		"mode": "fixed_fallback", "coin": "usdt", "base": cfg.Currency,
		"rate": 1 / cfg.FallbackPrice, "price": cfg.FallbackPrice,
		"expires_at": cfg.FallbackExpiresAt, "decided_at": now,
		"fallback_reason": reason, "verification": s,
	})
	if err != nil {
		return 0, ""
	}
	return 1 / cfg.FallbackPrice, string(raw)
}

// Serialized refreshes avoid publishing out of order and collapse concurrent
// order requests. Refresh is synchronous when due: detected anomalies cannot
// leak through a stale-while-revalidate return path.
func refreshVerifiedRate(cfg VerifiedRateConfig, force bool) (VerifiedRateState, error) {
	startedAt := rateNow()
	verifiedRateMu.Lock()
	defer verifiedRateMu.Unlock()
	id := verifiedConfigID(cfg)
	state := VerifiedRateState{Base: cfg.Currency, ConfigID: id}
	if cached, ok := verifiedRateMemory[cfg.Currency]; ok {
		state = cached
	} else if VerifiedRateLoad != nil {
		loaded, found, err := VerifiedRateLoad(cfg.Currency)
		if err != nil {
			return state, fmt.Errorf("cannot load verified quote state")
		}
		if found {
			state = loaded
		}
	}
	if state.ConfigID != id {
		state = VerifiedRateState{Base: cfg.Currency, ConfigID: id}
	}
	current, err := GetVerifiedRateConfig()
	if err != nil || !current.Enabled || verifiedConfigID(current) != id {
		return state, fmt.Errorf("verified rate configuration changed")
	}
	now := rateNow()
	if force && state.LastAttemptAt.After(startedAt) && state.FailureKind != "storage" {
		return state, nil
	}
	interval := time.Duration(cfg.RefreshSeconds) * time.Second
	if state.FailureKind != "" {
		interval = 60 * time.Second
	}
	if !force && !state.LastAttemptAt.IsZero() && !state.LastAttemptAt.After(now) && now.Before(state.LastAttemptAt.Add(interval)) && (state.FailureKind != "" || now.Before(state.ExpiresAt)) {
		return state, nil
	}

	primary, check, fetchErr := fetchVerifiedQuotes(cfg)
	now = rateNow()
	current, err = GetVerifiedRateConfig()
	if err != nil || !current.Enabled || verifiedConfigID(current) != id {
		return state, fmt.Errorf("verified rate configuration changed; response discarded")
	}
	state.LastAttemptAt = now
	state.ObservedPrimary, state.ObservedCheck = primary, check
	state.SpreadPercent = 0
	if positiveFinite(primary.Price) && positiveFinite(check.Price) {
		state.SpreadPercent = priceSpread(primary.Price, check.Price)
	}
	if fetchErr == nil {
		fetchErr = validateVerifiedQuotes(primary, check, state, cfg, now)
	}
	previous := state
	if fetchErr != nil {
		state.LastError = fetchErr.Message
		state.FailureKind = fetchErr.Kind
		// Network/429/5xx failure may use a SHORT, previously verified quote.
		// Any malformed quote or conflicting price pauses even an unexpired quote.
		if fetchErr.Kind != "unavailable" {
			state.Paused = true
		}
	} else {
		state.Primary, state.Check = primary, check
		state.AcceptedAt = now
		oldest := primary.QuotedAt
		if check.QuotedAt.Before(oldest) {
			oldest = check.QuotedAt
		}
		state.ExpiresAt = oldest.Add(time.Duration(cfg.MaxAgeSeconds) * time.Second)
		state.Paused = false
		state.FailureKind = ""
		state.LastError = ""
	}
	alert := state.LastError != "" && (state.LastAlertAt.IsZero() || now.Sub(state.LastAlertAt) >= 15*time.Minute)
	if alert {
		state.LastAlertAt = now
	}
	if VerifiedRateSave == nil || VerifiedRateSave(state) != nil {
		// Never expose an unpersisted accepted quote. Also remember a failed safety
		// write in memory so a subsequent read cannot silently revive the old cache.
		previous.Paused = true
		previous.FailureKind = "storage"
		previous.LastError = "cannot persist verified quote state"
		previous.LastAttemptAt = now
		verifiedRateMemory[cfg.Currency] = previous
		return previous, fmt.Errorf("cannot persist verified quote state")
	}
	verifiedRateMemory[cfg.Currency] = state
	if alert && VerifiedRateAlert != nil {
		VerifiedRateAlert("USDT " + strings.ToUpper(cfg.Currency) + " rate alert: " + state.LastError)
	}
	return state, nil
}

type quoteFailure struct {
	Kind    string
	Message string
}

func badQuote(message string) *quoteFailure { return &quoteFailure{"invalid_quote", message} }
func positiveFinite(v float64) bool         { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func priceSpread(a, b float64) float64 {
	spread := math.Abs(a-b) / math.Min(a, b) * 100
	// Keep rejected observations serializable so even a huge finite quote can
	// durably pause the engine. The cap still exceeds every allowed threshold.
	if math.IsInf(spread, 0) || math.IsNaN(spread) {
		return math.MaxFloat64
	}
	return spread
}
func relativeChange(a, b float64) float64 { return math.Abs(a-b) / b * 100 }

func validateVerifiedQuotes(p, q ProviderQuote, previous VerifiedRateState, c VerifiedRateConfig, now time.Time) *quoteFailure {
	for _, v := range []ProviderQuote{p, q} {
		if failure := validateProviderQuote(v, c, now); failure != nil {
			return failure
		}
	}
	if math.Abs(p.QuotedAt.Sub(q.QuotedAt).Seconds()) > float64(c.MaxTimeSkewSeconds) {
		return badQuote("provider timestamps are too far apart")
	}
	if priceSpread(p.Price, q.Price) > c.MaxSpreadPercent {
		return &quoteFailure{"price_conflict", "provider prices disagree"}
	}
	if positiveFinite(previous.Primary.Price) {
		if p.QuotedAt.Before(previous.Primary.QuotedAt) || q.QuotedAt.Before(previous.Check.QuotedAt) {
			return badQuote("provider timestamp moved backwards")
		}
		if relativeChange(p.Price, previous.Primary.Price) > c.MaxJumpPercent || relativeChange(q.Price, previous.Check.Price) > c.MaxJumpPercent {
			return &quoteFailure{"price_jump", "price moved too far from last accepted quote"}
		}
	}
	return nil
}

func verifiedStateUsable(s VerifiedRateState, c VerifiedRateConfig, now time.Time) bool {
	current, err := GetVerifiedRateConfig()
	return err == nil && current.Enabled && verifiedConfigID(current) == s.ConfigID && s.ConfigID == verifiedConfigID(c) && !s.Paused && !s.AcceptedAt.IsZero() && !s.AcceptedAt.After(now) && now.Before(s.ExpiresAt) && positiveFinite(s.Primary.Price) && validateVerifiedQuotes(s.Primary, s.Check, VerifiedRateState{}, c, now) == nil
}

type VerifiedRateStatus struct {
	Config                 VerifiedRateConfig `json:"config"`
	Mode                   string             `json:"mode"`
	CMCKeyConfigured       bool               `json:"cmc_key_configured"`
	CoinGeckoKeyConfigured bool               `json:"coingecko_key_configured"`
	State                  VerifiedRateState  `json:"state"`
	Usable                 bool               `json:"usable"`
	FallbackAvailable      bool               `json:"fallback_available"`
	Error                  string             `json:"error,omitempty"`
}

func GetVerifiedRateStatus() VerifiedRateStatus {
	cfg, err := GetVerifiedRateConfig()
	result := VerifiedRateStatus{Config: cfg, Mode: GetRateMode(), CMCKeyConfigured: cfg.CMCAPIKey != "", CoinGeckoKeyConfigured: cfg.CoinGeckoAPIKey != ""}
	result.Config.CMCAPIKey = ""
	result.Config.CoinGeckoAPIKey = ""
	if err != nil {
		result.Error = err.Error()
		return result
	}
	verifiedRateMu.Lock()
	defer verifiedRateMu.Unlock()
	if s, ok := verifiedRateMemory[cfg.Currency]; ok {
		result.State = s
	} else if VerifiedRateLoad != nil {
		s, _, e := VerifiedRateLoad(cfg.Currency)
		if e != nil {
			result.Error = "cannot load verified quote state"
			return result
		}
		result.State = s
	}
	if result.State.ConfigID != verifiedConfigID(cfg) {
		result.State = VerifiedRateState{Base: cfg.Currency}
	}
	result.Usable = cfg.Enabled && result.Mode == RateModeAuto && verifiedStateUsable(result.State, cfg, rateNow())
	result.FallbackAvailable = !result.Usable && fallbackUsable(result.State, cfg, rateNow())
	return result
}

func RefreshVerifiedRate() VerifiedRateStatus {
	cfg, err := GetVerifiedRateConfig()
	if err == nil && cfg.Enabled {
		_, err = refreshVerifiedRate(cfg, true)
	}
	result := GetVerifiedRateStatus()
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func validateProviderQuote(v ProviderQuote, c VerifiedRateConfig, now time.Time) *quoteFailure {

	if v.Currency != c.Currency || !positiveFinite(v.Price) || !positiveFinite(1/v.Price) {
		return badQuote("invalid currency or price")
	}
	// Future timestamps are not accepted; fetching again cannot extend source age.
	if v.QuotedAt.IsZero() || v.QuotedAt.After(now) || !now.Before(v.QuotedAt.Add(time.Duration(c.MaxAgeSeconds)*time.Second)) {
		return badQuote(v.Provider + ": quote timestamp is stale or in the future")
	}
	if v.Price < c.MinPrice || v.Price > c.MaxPrice {
		return &quoteFailure{"price_range", v.Provider + ": price outside administrator limits"}
	}
	if relativeChange(v.Price, c.ReferencePrice) > c.MaxJumpPercent {
		return &quoteFailure{"price_jump", v.Provider + ": price differs too far from administrator reference"}
	}
	return nil
}

const verifiedSourceName = "verified-usdt:coinmarketcap+coingecko"

func refreshVerifiedRateBase(base string, cfg VerifiedRateConfig, force bool) RateRefreshResult {
	base = normalizeRateKey(base)
	if base != cfg.Currency {
		return RateRefreshResult{Base: base, SourceURL: verifiedSourceName, Error: "unsupported verified billing currency"}
	}
	state, err := refreshVerifiedRate(cfg, force)
	result := RateRefreshResult{Base: base, SourceURL: verifiedSourceName, OK: err == nil && state.LastError == "" && verifiedStateUsable(state, cfg, rateNow()), Error: state.LastError}
	if err != nil {
		result.Error = err.Error()
	}
	if positiveFinite(state.Primary.Price) {
		result.Rates = map[string]float64{"usdt": 1 / state.Primary.Price}
	}
	if !state.AcceptedAt.IsZero() {
		result.LastSuccessAt = timePointer(state.AcceptedAt)
	}
	if !state.LastAttemptAt.IsZero() {
		result.LastAttemptAt = timePointer(state.LastAttemptAt)
	}
	if !state.ExpiresAt.IsZero() {
		result.ExpiresAt = timePointer(state.ExpiresAt)
	}
	return result
}
func verifiedLegacyStatus() RateStatus {
	verified := GetVerifiedRateStatus()
	state := verified.State
	base := RateBaseStatus{Base: verified.Config.Currency, SourceURL: verifiedSourceName, IsUsable: verified.Usable, IsStale: !verified.Usable || !rateNow().Before(state.AcceptedAt.Add(time.Duration(verified.Config.RefreshSeconds)*time.Second)), LastRefreshOK: !state.AcceptedAt.IsZero() && state.LastError == "" && verified.Error == "", LastError: state.LastError}
	if verified.Error != "" {
		base.LastError = verified.Error
	}
	if positiveFinite(state.Primary.Price) {
		base.Rates = map[string]float64{"usdt": 1 / state.Primary.Price}
	}
	if !state.AcceptedAt.IsZero() {
		base.LastSuccessAt = timePointer(state.AcceptedAt)
	}
	if !state.LastAttemptAt.IsZero() {
		base.LastAttemptAt = timePointer(state.LastAttemptAt)
	}
	if !state.ExpiresAt.IsZero() {
		base.ExpiresAt = timePointer(state.ExpiresAt)
	}
	return RateStatus{Mode: GetRateMode(), APIURL: verifiedSourceName, CacheTTLSeconds: verified.Config.RefreshSeconds, Bases: []RateBaseStatus{base}, Verified: &verified}
}
