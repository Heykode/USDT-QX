package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GMWalletApp/epusdt/util/http_client"
)

func fetchVerifiedQuotes(c VerifiedRateConfig) (ProviderQuote, ProviderQuote, *quoteFailure) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var p, q ProviderQuote
	var pe, qe *quoteFailure
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p, pe = fetchCMCQuote(ctx, c) }()
	go func() { defer wg.Done(); q, qe = fetchCoinGeckoQuote(ctx, c) }()
	wg.Wait()
	if pe == nil {
		pe = validateProviderQuote(p, c, rateNow())
	}
	if qe == nil {
		qe = validateProviderQuote(q, c, rateNow())
	}
	// Prefer a known bad quote over a simultaneous network error.
	for _, e := range []*quoteFailure{pe, qe} {
		if e != nil && e.Kind != "unavailable" {
			return p, q, e
		}
	}
	if pe != nil {
		return p, q, pe
	}
	return p, q, qe
}

func providerJSON(ctx context.Context, endpoint, header, key string, query map[string]string, out any) *quoteFailure {
	// Fixed official HTTPS hosts only. Never forward credentials across redirects,
	// print response bodies, or interpolate transport errors that may contain keys.
	client := http_client.GetHttpClient()
	client.SetDebug(false).SetRetryCount(0)
	client.GetClient().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.R().SetContext(ctx).SetHeader(header, key).SetQueryParams(query).SetDoNotParseResponse(true).Get(endpoint)
	if err != nil {
		return &quoteFailure{"unavailable", "provider request failed"}
	}
	defer resp.RawBody().Close()
	code := resp.StatusCode()
	if code == 429 || code >= 500 {
		return &quoteFailure{"unavailable", fmt.Sprintf("provider HTTP %d", code)}
	}
	if code != 200 {
		return badQuote(fmt.Sprintf("provider HTTP %d", code))
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.RawBody(), (1<<20)+1))
	if readErr != nil {
		return &quoteFailure{"unavailable", "provider body read failed"}
	}
	if len(body) > 1<<20 {
		return badQuote("provider response too large")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return badQuote("provider response has invalid JSON")
	}
	return nil
}

func fetchCMCQuote(ctx context.Context, c VerifiedRateConfig) (ProviderQuote, *quoteFailure) {
	q := ProviderQuote{Provider: "coinmarketcap", Currency: c.Currency}
	var payload struct {
		Status struct {
			ErrorCode json.RawMessage `json:"error_code"`
		} `json:"status"`
		Data []struct {
			ID     int    `json:"id"`
			Symbol string `json:"symbol"`
			Quote  []struct {
				Symbol      string    `json:"symbol"`
				Price       float64   `json:"price"`
				LastUpdated time.Time `json:"last_updated"`
			} `json:"quote"`
		} `json:"data"`
	}
	err := providerJSON(ctx, "https://pro-api.coinmarketcap.com/v3/cryptocurrency/quotes/latest", "X-CMC_PRO_API_KEY", c.CMCAPIKey, map[string]string{"id": "825", "convert": strings.ToUpper(c.Currency)}, &payload)
	if err != nil {
		err.Message = "CoinMarketCap: " + err.Message
		return q, err
	}
	status := string(payload.Status.ErrorCode)
	if status != "0" && status != `"0"` {
		return q, badQuote("CoinMarketCap: missing or unsuccessful status")
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != 825 || payload.Data[0].Symbol != "USDT" || len(payload.Data[0].Quote) != 1 {
		return q, badQuote("CoinMarketCap: unexpected asset or duplicate quotes")
	}
	v := payload.Data[0].Quote[0]
	if v.Symbol != strings.ToUpper(c.Currency) {
		return q, badQuote("CoinMarketCap: unexpected quote currency")
	}
	q.Price, q.QuotedAt = v.Price, v.LastUpdated
	return q, nil
}

func fetchCoinGeckoQuote(ctx context.Context, c VerifiedRateConfig) (ProviderQuote, *quoteFailure) {
	q := ProviderQuote{Provider: "coingecko", Currency: c.Currency}
	endpoint, header := "https://pro-api.coingecko.com/api/v3/simple/price", "x-cg-pro-api-key"
	if c.CoinGeckoPlan == "demo" {
		endpoint, header = "https://api.coingecko.com/api/v3/simple/price", "x-cg-demo-api-key"
	}
	var payload map[string]map[string]json.RawMessage
	err := providerJSON(ctx, endpoint, header, c.CoinGeckoAPIKey, map[string]string{"ids": "tether", "vs_currencies": c.Currency, "include_last_updated_at": "true", "precision": "full"}, &payload)
	if err != nil {
		err.Message = "CoinGecko: " + err.Message
		return q, err
	}
	values, ok := payload["tether"]
	if !ok || len(payload) != 1 {
		return q, badQuote("CoinGecko: unexpected asset")
	}
	if err := json.Unmarshal(values[c.Currency], &q.Price); err != nil {
		return q, badQuote("CoinGecko: missing or invalid price")
	}
	var timestamp int64
	if err := json.Unmarshal(values["last_updated_at"], &timestamp); err != nil || timestamp <= 0 {
		return q, badQuote("CoinGecko: missing or invalid timestamp")
	}
	q.QuotedAt = time.Unix(timestamp, 0).UTC()
	return q, nil
}
