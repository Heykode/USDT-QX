package config

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GMWalletApp/epusdt/util/http_client"
	"golang.org/x/sync/singleflight"
)

const (
	RateModeFixed          = "fixed"
	RateModeAuto           = "auto"
	DefaultRateCacheTTL    = 300
	MinRateCacheTTLSeconds = 10
	MaxRateCacheTTLSeconds = 86400
	// This source publishes daily reference rates, not live exchange quotes.
	DefaultRateAPIURL = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies"
	MaxRateQuoteAge   = 48 * time.Hour
)

type RateRefreshResult struct {
	Base          string             `json:"base" example:"cny"`
	OK            bool               `json:"ok"`
	Skipped       bool               `json:"skipped,omitempty"`
	SourceURL     string             `json:"source_url"`
	QuoteDate     string             `json:"quote_date,omitempty"`
	ExpiresAt     *time.Time         `json:"expires_at,omitempty"`
	Rates         map[string]float64 `json:"rates,omitempty"`
	LastSuccessAt *time.Time         `json:"last_success_at,omitempty" example:"2026-07-22T10:00:00Z"`
	LastAttemptAt *time.Time         `json:"last_attempt_at,omitempty" example:"2026-07-22T10:05:00Z"`
	Error         string             `json:"error,omitempty" example:"call rate api unexpected status: 502 Bad Gateway"`
}

type RateBaseStatus struct {
	Base          string             `json:"base" example:"cny"`
	Rates         map[string]float64 `json:"rates"`
	SourceURL     string             `json:"source_url" example:"https://cdn.example.com/currencies/"`
	IsStale       bool               `json:"is_stale"`
	IsUsable      bool               `json:"is_usable"`
	QuoteDate     string             `json:"quote_date,omitempty"`
	ExpiresAt     *time.Time         `json:"expires_at,omitempty"`
	LastSuccessAt *time.Time         `json:"last_success_at,omitempty" example:"2026-07-22T10:00:00Z"`
	LastAttemptAt *time.Time         `json:"last_attempt_at,omitempty" example:"2026-07-22T10:05:00Z"`
	LastRefreshOK bool               `json:"last_refresh_ok"`
	LastError     string             `json:"last_error" example:"call rate api unexpected status: 502 Bad Gateway"`
}

type RateStatus struct {
	Verified        *VerifiedRateStatus `json:"verified,omitempty"`
	Mode            string              `json:"mode" enums:"fixed,auto" example:"auto"`
	APIURL          string              `json:"api_url" example:"https://cdn.example.com/currencies/"`
	CacheTTLSeconds int                 `json:"cache_ttl_seconds" minimum:"10" maximum:"86400" example:"300"`
	Bases           []RateBaseStatus    `json:"bases"`
}

var (
	rateCacheMu        sync.RWMutex
	rateCacheMemory    = make(map[string]RateCacheSnapshot)
	rateRefreshes      singleflight.Group
	ratePublishMu      sync.Mutex
	rateAsyncRefreshes sync.WaitGroup
	rateNow            = time.Now
)

func GetRateMode() string {
	if strings.EqualFold(strings.TrimSpace(settingsRateMode()), RateModeAuto) {
		return RateModeAuto
	}
	return RateModeFixed
}

func GetRateCacheTTLSeconds() int {
	ttl, err := strconv.Atoi(strings.TrimSpace(settingsRateCacheTTLSeconds()))
	if err != nil || ttl < MinRateCacheTTLSeconds || ttl > MaxRateCacheTTLSeconds {
		return DefaultRateCacheTTL
	}
	return ttl
}

func GetRateForCoin(coin string, base string) float64 {
	rate, _ := GetPaymentRate(coin, base)
	return rate
}

func getLegacyRateForCoin(coin string, base string, mode string) float64 {
	coin = normalizeRateKey(coin)
	base = normalizeRateKey(base)
	if coin == "" || base == "" {
		return 0
	}
	if coin == base {
		return 1
	}
	if mode == RateModeFixed {
		if forcedRate := getForcedRateForCoin(coin, base); forcedRate > 0 {
			return forcedRate
		}
		if coin == "usdt" && base == "usd" {
			return 1
		}
		return 0
	}
	if coin == "usdt" && base == "usd" {
		return 1
	}

	snapshot, found := loadRateCacheSnapshot(base)
	rate := rateFromSnapshot(snapshot, coin)
	now := rateNow()
	if found && rate > 0 && isRateCacheUsable(snapshot, now) {
		if isRateCacheStale(snapshot, now) && isRateRefreshDue(snapshot, now) {
			rateAsyncRefreshes.Add(1)
			go func() {
				defer rateAsyncRefreshes.Done()
				_ = refreshRateBaseForCoin(base, coin, false)
			}()
		}
		if isRateCacheUsable(snapshot, rateNow()) {
			return rate
		}
		return 0
	}

	// A successful, fresh full-base payload that does not contain the target
	// coin is authoritative until its TTL expires.
	if found && isRateCacheUsable(snapshot, now) && !isRateCacheStale(snapshot, now) {
		return 0
	}
	_ = refreshRateBaseForCoin(base, coin, false)
	snapshot, _ = loadRateCacheSnapshot(base)
	if !isRateCacheUsable(snapshot, rateNow()) {
		return 0
	}
	return rateFromSnapshot(snapshot, coin)
}

func GetUsdtRate() float64 {
	rate := GetRateForCoin("usdt", "cny")
	if rate <= 0 {
		log.Printf("usdt/cny rate unavailable in %s mode", GetRateMode())
		return 0
	}
	return 1 / rate
}

func RefreshRateBase(base string, force bool) RateRefreshResult {
	if cfg, err := GetVerifiedRateConfig(); GetRateMode() == RateModeAuto && (err != nil || cfg.Enabled) {
		if err != nil {
			return RateRefreshResult{Base: normalizeRateKey(base), Error: err.Error()}
		}
		return refreshVerifiedRateBase(base, cfg, force)
	}
	return refreshRateBaseForCoin(base, "", force)
}

func refreshRateBaseForCoin(base string, requiredCoin string, force bool) RateRefreshResult {
	base = normalizeRateKey(base)
	requiredCoin = normalizeRateKey(requiredCoin)
	if base == "" {
		return RateRefreshResult{Base: base, Error: "base currency is empty"}
	}
	source := GetRateApiUrl()
	value, _, _ := rateRefreshes.Do(base+"\x00"+source, func() (interface{}, error) {
		// Recheck after joining singleflight: another caller may have completed
		// between our initial cache lookup and entering this function.
		snapshot, _ := loadRateCacheSnapshot(base)
		now := rateNow()
		if !force && normalizeRateSource(snapshot.SourceURL) == source {
			if isRateCacheUsable(snapshot, now) && !isRateCacheStale(snapshot, now) {
				return refreshResultFromSnapshot(snapshot, true), nil
			}
			// A hard expiry overrides the successful-cache refresh interval.
			// Failed attempts remain throttled, including a failed cold start.
			if !snapshot.LastRefreshOK && !isRateRefreshDue(snapshot, now) {
				return refreshResultFromSnapshot(snapshot, true), nil
			}
		}
		return refreshRateBase(base, requiredCoin, source), nil
	})
	return value.(RateRefreshResult)
}

func refreshRateBase(base string, requiredCoin string, source string) RateRefreshResult {
	rates, quoteDate, expiresAt, err := fetchRatesForBase(base, source)
	attemptedAt := rateNow()
	// Serialize publication so an old in-flight source cannot overwrite a
	// newly selected source after that source has already published its quote.
	ratePublishMu.Lock()
	defer ratePublishMu.Unlock()
	if GetRateApiUrl() != source {
		return RateRefreshResult{Base: base, SourceURL: source, Error: "rate source changed during refresh; response discarded"}
	}
	snapshot, _ := loadRateCacheSnapshot(base)
	if normalizeRateSource(snapshot.SourceURL) != source {
		// Never relabel the previous provider's rates as the new provider's.
		snapshot = RateCacheSnapshot{Base: base, SourceURL: source}
	}
	previous := cloneSnapshot(snapshot)
	snapshot.Base = base
	snapshot.LastAttemptAt = timePointer(attemptedAt)
	if err == nil && requiredCoin != "" && rateFromRates(rates, requiredCoin) <= 0 {
		err = fmt.Errorf("rate api response has no positive %s.%s rate", base, requiredCoin)
	}
	if err != nil {
		snapshot.LastRefreshOK = false
		snapshot.LastError = err.Error()
		if saveErr := storeRateCacheSnapshot(snapshot); saveErr != nil {
			snapshot.LastError += "; persist cache status: " + saveErr.Error()
			cacheRateSnapshotInMemory(snapshot)
		}
		log.Printf("refresh rate cache for %s failed: %s", base, snapshot.LastError)
		return refreshResultFromSnapshot(snapshot, false)
	}

	// Only rates present in this payload receive its validity period. Merging
	// missing coins would repeatedly renew potentially much older quotes.
	snapshot.Rates = rates
	snapshot.SourceURL = source
	snapshot.QuoteDate = quoteDate
	snapshot.ExpiresAt = expiresAt
	snapshot.LastSuccessAt = timePointer(attemptedAt)
	snapshot.LastRefreshOK = true
	snapshot.LastError = ""
	if err := storeRateCacheSnapshot(snapshot); err != nil {
		// Do not publish an API result that was not persisted. Continue serving
		// the last durable snapshot and expose the failed attempt in memory.
		previous.Base = base
		previous.LastAttemptAt = timePointer(attemptedAt)
		previous.LastRefreshOK = false
		previous.LastError = "persist rate cache: " + err.Error()
		cacheRateSnapshotInMemory(previous)
		return refreshResultFromSnapshot(previous, false)
	}
	return refreshResultFromSnapshot(snapshot, false)
}

func fetchRatesForBase(base string, baseURL string) (map[string]float64, string, *time.Time, error) {
	requestURL := strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(base) + ".json"
	resp, err := http_client.GetHttpClient().R().Get(requestURL)
	if err != nil {
		return nil, "", nil, fmt.Errorf("call rate api: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, "", nil, fmt.Errorf("call rate api unexpected status: %s", resp.Status())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return nil, "", nil, fmt.Errorf("decode rate api response: %w", err)
	}
	quoteDate, expiresAt, err := rateQuoteValidity(payload, baseURL, rateNow())
	if err != nil {
		return nil, "", nil, err
	}
	var rawBase json.RawMessage
	for key, candidate := range payload {
		if normalizeRateKey(key) != base {
			continue
		}
		if rawBase != nil {
			return nil, "", nil, fmt.Errorf("rate api response contains duplicate normalized base %s", base)
		}
		rawBase = candidate
	}
	if rawBase == nil {
		return nil, "", nil, fmt.Errorf("rate api response has no %s object", base)
	}
	var rawRates map[string]float64
	if err := json.Unmarshal(rawBase, &rawRates); err != nil {
		return nil, "", nil, fmt.Errorf("decode %s rate object: %w", base, err)
	}
	rates := make(map[string]float64, len(rawRates))
	for coin, rate := range rawRates {
		coin = normalizeRateKey(coin)
		if coin == "" || rate <= 0 {
			continue
		}
		if _, exists := rates[coin]; exists {
			return nil, "", nil, fmt.Errorf("rate api response contains duplicate normalized coin %s.%s", base, coin)
		}
		rates[coin] = rate
	}
	if len(rates) == 0 {
		return nil, "", nil, fmt.Errorf("rate api response has no positive %s rates", base)
	}
	return rates, quoteDate, expiresAt, nil
}

func rateQuoteValidity(payload map[string]json.RawMessage, source string, now time.Time) (string, *time.Time, error) {
	rawDate, hasDate := payload["date"]
	if !hasDate {
		if source == DefaultRateAPIURL {
			return "", nil, fmt.Errorf("default rate source response is missing date")
		}
		// Legacy custom endpoints may omit date. In that case we can bound
		// cache age only, not verify the provider's underlying quote age.
		return "", timePointer(now.Add(MaxRateQuoteAge)), nil
	}
	var quoteDate string
	if err := json.Unmarshal(rawDate, &quoteDate); err != nil {
		return "", nil, fmt.Errorf("rate api date must be YYYY-MM-DD")
	}
	day, err := time.Parse(time.DateOnly, quoteDate)
	if err != nil {
		return "", nil, fmt.Errorf("rate api date must be YYYY-MM-DD")
	}
	if day.After(now.UTC()) {
		return "", nil, fmt.Errorf("rate api date is in the future")
	}
	expiresAt := day.Add(MaxRateQuoteAge)
	if !now.Before(expiresAt) {
		return "", nil, fmt.Errorf("rate api quote is older than the 48-hour limit")
	}
	return quoteDate, &expiresAt, nil
}

func GetRateStatus() RateStatus {
	if cfg, err := GetVerifiedRateConfig(); GetRateMode() == RateModeAuto && (err != nil || cfg.Enabled) {
		return verifiedLegacyStatus()
	}
	snapshots := listRateCacheSnapshots()
	now := rateNow()
	bases := make([]RateBaseStatus, 0, len(snapshots))
	for _, snapshot := range snapshots {
		bases = append(bases, RateBaseStatus{
			Base:          snapshot.Base,
			Rates:         cloneRates(snapshot.Rates),
			SourceURL:     snapshot.SourceURL,
			IsStale:       isRateCacheStale(snapshot, now),
			IsUsable:      isRateCacheUsable(snapshot, now),
			QuoteDate:     snapshot.QuoteDate,
			ExpiresAt:     snapshot.ExpiresAt,
			LastSuccessAt: snapshot.LastSuccessAt,
			LastAttemptAt: snapshot.LastAttemptAt,
			LastRefreshOK: snapshot.LastRefreshOK,
			LastError:     snapshot.LastError,
		})
	}
	return RateStatus{
		Mode:            GetRateMode(),
		APIURL:          GetRateApiUrl(),
		CacheTTLSeconds: GetRateCacheTTLSeconds(),
		Bases:           bases,
	}
}

func KnownRateBases() []string {
	if cfg, err := GetVerifiedRateConfig(); err == nil && cfg.Enabled && GetRateMode() == RateModeAuto {
		return []string{cfg.Currency}
	}
	set := make(map[string]struct{})
	for _, snapshot := range listRateCacheSnapshots() {
		if snapshot.Base != "" {
			set[snapshot.Base] = struct{}{}
		}
	}
	var forced map[string]json.RawMessage
	if json.Unmarshal([]byte(settingsForcedRateList()), &forced) == nil {
		for base := range forced {
			if normalized := normalizeRateKey(base); normalized != "" {
				set[normalized] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		set["cny"] = struct{}{}
	}
	bases := make([]string, 0, len(set))
	for base := range set {
		bases = append(bases, base)
	}
	sort.Strings(bases)
	return bases
}

func ResetRateCacheRuntime() {
	resetVerifiedRateMemory()
	rateCacheMu.Lock()
	rateCacheMemory = make(map[string]RateCacheSnapshot)
	rateCacheMu.Unlock()
}

func getForcedRateForCoin(coin string, base string) float64 {
	var rates map[string]map[string]float64
	if json.Unmarshal([]byte(settingsForcedRateList()), &rates) != nil {
		return 0
	}
	for configuredBase, coins := range rates {
		if normalizeRateKey(configuredBase) != base {
			continue
		}
		for configuredCoin, rate := range coins {
			if normalizeRateKey(configuredCoin) == coin && rate > 0 {
				return rate
			}
		}
	}
	return 0
}

func loadRateCacheSnapshot(base string) (RateCacheSnapshot, bool) {
	rateCacheMu.RLock()
	snapshot, ok := rateCacheMemory[base]
	rateCacheMu.RUnlock()
	if ok {
		return cloneSnapshot(snapshot), true
	}
	if RateCacheLoad == nil {
		return RateCacheSnapshot{Base: base}, false
	}
	loaded, err := RateCacheLoad(base)
	if err != nil {
		return RateCacheSnapshot{Base: base}, false
	}
	loaded.Base = base
	rateCacheMu.Lock()
	if current, exists := rateCacheMemory[base]; exists {
		rateCacheMu.Unlock()
		return cloneSnapshot(current), true
	}
	rateCacheMemory[base] = cloneSnapshot(loaded)
	rateCacheMu.Unlock()
	return cloneSnapshot(loaded), true
}

func listRateCacheSnapshots() []RateCacheSnapshot {
	if RateCacheLoadAll != nil {
		if loaded, err := RateCacheLoadAll(); err == nil {
			rateCacheMu.Lock()
			for _, snapshot := range loaded {
				snapshot.Base = normalizeRateKey(snapshot.Base)
				if snapshot.Base == "" {
					continue
				}
				current, exists := rateCacheMemory[snapshot.Base]
				if !exists || isSnapshotNewer(snapshot, current) {
					rateCacheMemory[snapshot.Base] = cloneSnapshot(snapshot)
				}
			}
			rateCacheMu.Unlock()
		}
	}
	rateCacheMu.RLock()
	out := make([]RateCacheSnapshot, 0, len(rateCacheMemory))
	for _, snapshot := range rateCacheMemory {
		out = append(out, cloneSnapshot(snapshot))
	}
	rateCacheMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out
}

func storeRateCacheSnapshot(snapshot RateCacheSnapshot) error {
	snapshot.Base = normalizeRateKey(snapshot.Base)
	if RateCacheSave != nil {
		if err := RateCacheSave(snapshot); err != nil {
			return err
		}
	}
	cacheRateSnapshotInMemory(snapshot)
	return nil
}

func cacheRateSnapshotInMemory(snapshot RateCacheSnapshot) {
	rateCacheMu.Lock()
	rateCacheMemory[snapshot.Base] = cloneSnapshot(snapshot)
	rateCacheMu.Unlock()
}

func rateFromSnapshot(snapshot RateCacheSnapshot, coin string) float64 {
	return rateFromRates(snapshot.Rates, coin)
}

func rateFromRates(rates map[string]float64, coin string) float64 {
	if rate := rates[coin]; rate > 0 {
		return rate
	}
	if coin == "ton" {
		return rates["toncoin"]
	}
	return 0
}

func normalizeRateSource(source string) string {
	return strings.TrimRight(strings.TrimSpace(source), "/")
}

func isRateCacheUsable(snapshot RateCacheSnapshot, now time.Time) bool {
	return snapshot.LastSuccessAt != nil && !snapshot.LastSuccessAt.After(now) &&
		snapshot.ExpiresAt != nil && now.Before(*snapshot.ExpiresAt) &&
		len(snapshot.Rates) > 0 && normalizeRateSource(snapshot.SourceURL) == GetRateApiUrl()
}

func isSnapshotNewer(candidate, current RateCacheSnapshot) bool {
	candidateTime := latestSnapshotTime(candidate)
	currentTime := latestSnapshotTime(current)
	return candidateTime.After(currentTime)
}

func latestSnapshotTime(snapshot RateCacheSnapshot) time.Time {
	latest := time.Time{}
	if snapshot.LastSuccessAt != nil && snapshot.LastSuccessAt.After(latest) {
		latest = *snapshot.LastSuccessAt
	}
	if snapshot.LastAttemptAt != nil && snapshot.LastAttemptAt.After(latest) {
		latest = *snapshot.LastAttemptAt
	}
	return latest
}

func isRateCacheStale(snapshot RateCacheSnapshot, now time.Time) bool {
	if !isRateCacheUsable(snapshot, now) {
		return true
	}
	return !now.Before(snapshot.LastSuccessAt.Add(time.Duration(GetRateCacheTTLSeconds()) * time.Second))
}

func isRateRefreshDue(snapshot RateCacheSnapshot, now time.Time) bool {
	if snapshot.LastAttemptAt == nil {
		return true
	}
	return !now.Before(snapshot.LastAttemptAt.Add(time.Duration(GetRateCacheTTLSeconds()) * time.Second))
}

func refreshResultFromSnapshot(snapshot RateCacheSnapshot, skipped bool) RateRefreshResult {
	return RateRefreshResult{
		Base:          snapshot.Base,
		OK:            snapshot.LastRefreshOK,
		Skipped:       skipped,
		SourceURL:     snapshot.SourceURL,
		QuoteDate:     snapshot.QuoteDate,
		ExpiresAt:     snapshot.ExpiresAt,
		Rates:         cloneRates(snapshot.Rates),
		LastSuccessAt: snapshot.LastSuccessAt,
		LastAttemptAt: snapshot.LastAttemptAt,
		Error:         snapshot.LastError,
	}
}

func normalizeRateKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneSnapshot(snapshot RateCacheSnapshot) RateCacheSnapshot {
	snapshot.Rates = cloneRates(snapshot.Rates)
	return snapshot
}

func cloneRates(rates map[string]float64) map[string]float64 {
	if rates == nil {
		return map[string]float64{}
	}
	cloned := make(map[string]float64, len(rates))
	for coin, rate := range rates {
		cloned[coin] = rate
	}
	return cloned
}

func timePointer(value time.Time) *time.Time {
	return &value
}
