package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
)

func TestVerifiedRateDiscountAndOldOrderSurvivePause(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	oldLoad, oldSave, oldAlert := config.VerifiedRateLoad, config.VerifiedRateSave, config.VerifiedRateAlert
	config.VerifiedRateLoad = data.LoadVerifiedRateState
	config.VerifiedRateSave = data.SaveVerifiedRateState
	config.VerifiedRateAlert = nil
	defer func() {
		config.VerifiedRateLoad = oldLoad
		config.VerifiedRateSave = oldSave
		config.VerifiedRateAlert = oldAlert
		config.ResetRateCacheRuntime()
	}()
	cfg := config.DefaultVerifiedRateConfig()
	cfg.Enabled = true
	cfg.CMCAPIKey = "fake-cmc"
	cfg.CoinGeckoAPIKey = "fake-cg"
	cfg.ReferencePrice = 8
	cfg.MinPrice = 6
	cfg.MaxPrice = 10
	raw, _ := json.Marshal(cfg)
	for k, v := range map[string]string{"rate.mode": "auto", config.VerifiedRateSettingKey: string(raw)} {
		if err := data.SetSetting("rate", k, v, "string"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := data.AddWalletAddress("verified-contract-wallet"); err != nil {
		t.Fatal(err)
	}
	quoteTime := time.Now().UTC().Truncate(time.Second)
	checkPrice := 8.0
	offline := false
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		status := 200
		if offline {
			status = 503
		}
		body := fmt.Sprintf(`{"status":{"error_code":0},"data":[{"id":825,"symbol":"USDT","quote":[{"symbol":"CNY","price":8,"last_updated":%q}]}]}`, quoteTime.Format(time.RFC3339))
		if r.URL.Host == "pro-api.coingecko.com" {
			body = fmt.Sprintf(`{"tether":{"cny":%g,"last_updated_at":%d}}`, checkPrice, quoteTime.Unix())
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	// New API sends the discounted payable amount: 100 at 10% off = 90.
	first, err := CreateTransaction(newCreateTransactionRequest("verified-discount", 90), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Amount != 90 || first.ActualAmount != 11.25 {
		t.Fatalf("wrong discounted quote: %+v", first)
	}
	persisted, err := data.GetOrderInfoByTradeId(first.TradeId)
	if err != nil || !strings.Contains(persisted.RateQuote, `"auto_verified"`) || !strings.Contains(persisted.RateQuote, `"rate":0.125`) {
		t.Fatalf("missing exact order audit: %v", err)
	}
	audit := persisted.RateQuote
	checkPrice = 8.1
	if result := config.RefreshVerifiedRate(); !result.State.Paused {
		t.Fatal("price conflict did not pause")
	}
	config.ResetRateCacheRuntime()
	if _, err := CreateTransaction(newCreateTransactionRequest("verified-paused", 90), nil); err == nil {
		t.Fatal("created new order with conflicting prices")
	}
	var count int64
	dao.Mdb.Model(&mdb.Orders{}).Where("order_id = ?", "verified-paused").Count(&count)
	if count != 0 {
		t.Fatal("rejected quote left an order behind")
	}
	checkout, err := GetCheckoutCounterByTradeId(first.TradeId)
	if err != nil || checkout.ActualAmount != 11.25 || checkout.ReceiveAddress != first.ReceiveAddress {
		t.Fatal("pause altered existing checkout")
	}
	owner, err := data.GetTradeIdByWalletAddressAndAmountAndToken("tron", first.ReceiveAddress, "USDT", first.ActualAmount)
	if err != nil || owner != first.TradeId {
		t.Fatal("pause released existing reservation")
	}
	persisted, _ = data.GetOrderInfoByTradeId(first.TradeId)
	if persisted.RateQuote != audit {
		t.Fatal("existing order audit changed")
	}
	// A healthy refresh restores the quote; a subsequent 503 may use it briefly.
	checkPrice = 8
	config.RefreshVerifiedRate()
	offline = true
	config.RefreshVerifiedRate()
	if _, err := CreateTransaction(newCreateTransactionRequest("verified-brief-outage", 90), nil); err != nil {
		t.Fatalf("brief outage discarded valid verified quote: %v", err)
	}
	state, _, err := data.LoadVerifiedRateState("cny")
	if err != nil {
		t.Fatal(err)
	}
	state.ExpiresAt = time.Now().Add(-time.Second)
	if err = data.SaveVerifiedRateState(state); err != nil {
		t.Fatal(err)
	}
	config.ResetRateCacheRuntime()
	if _, err := CreateTransaction(newCreateTransactionRequest("verified-expired", 90), nil); err == nil {
		t.Fatal("expired cached quote allowed new order")
	}
	// Enable per-order fallback, then prove a real order stores the decision and
	// the immediately following order recovers without a global mode change.
	cfg.FallbackEnabled = true
	cfg.FallbackPrice = 7.5
	raw, _ = json.Marshal(cfg)
	if err := data.SetSetting("rate", config.VerifiedRateSettingKey, string(raw), "json"); err != nil {
		t.Fatal(err)
	}
	fixed, err := CreateTransaction(newCreateTransactionRequest("verified-fixed-fallback", 90), nil)
	if err != nil || fixed.ActualAmount != 12 {
		t.Fatalf("discounted fallback failed: %v", err)
	}
	fixedRow, _ := data.GetOrderInfoByTradeId(fixed.TradeId)
	if !strings.Contains(fixedRow.RateQuote, `"fixed_fallback"`) || !strings.Contains(fixedRow.RateQuote, `"fallback_reason"`) {
		t.Fatal("order omitted fallback audit")
	}
	offline = false
	recovered, err := CreateTransaction(newCreateTransactionRequest("verified-auto-recovered", 80), nil)
	if err != nil || recovered.ActualAmount != 10 {
		t.Fatalf("next order did not recover automatic rate: %v", err)
	}
	fixedCheckout, err := GetCheckoutCounterByTradeId(fixed.TradeId)
	if err != nil || fixedCheckout.ActualAmount != 12 {
		t.Fatal("automatic recovery repriced fixed order")
	}
}
