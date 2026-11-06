package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/data"
)

// New API applies its discount before passing the payable amount to this
// gateway. Changing the external quote must only affect newly created orders.
func TestQXAutomaticRateUsesDiscountedAmountAndPreservesExistingOrder(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	for key, value := range map[string]string{"rate.mode": "auto", "rate.api_url": "https://rates.example.test/v1/currencies", "rate.cache_ttl_seconds": "300"} {
		if err := data.SetSetting("rate", key, value, "string"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := data.AddWalletAddress("qx-rate-test-wallet"); err != nil {
		t.Fatal(err)
	}
	quote := 0.125 // 1 CNY = 0.125 USDT, or 1 USDT = 8 CNY.
	offline := false
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if offline {
			return nil, errors.New("quote provider offline")
		}
		if r.URL.Host != "rates.example.test" || r.URL.Path != "/v1/currencies/cny.json" {
			t.Fatalf("unexpected rate provider request: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"cny":{"usdt":%f}}`, quote))), Request: r}, nil
	})
	first, err := CreateTransaction(newCreateTransactionRequest("qx-discounted-order", 90), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Amount != 90 || first.ActualAmount != 11.25 {
		t.Fatalf("discounted amount/USDT = %v/%v, want 90/11.25", first.Amount, first.ActualAmount)
	}
	quote = 0.2
	if result := config.RefreshRateBase("cny", true); !result.OK {
		t.Fatalf("refresh rate: %+v", result)
	}
	second, err := CreateTransaction(newCreateTransactionRequest("qx-new-rate-order", 90), nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ActualAmount != 18 {
		t.Fatalf("new order amount = %v, want 18", second.ActualAmount)
	}
	// An unavailable provider and hard-expired cache must block new orders
	// while the already reserved payment target remains intact.
	snapshot, err := data.LoadRateCacheSnapshot("cny")
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Second)
	snapshot.ExpiresAt = &expired
	if err := data.SaveRateCacheSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	offline = true
	config.ResetRateCacheRuntime()
	if order, err := CreateTransaction(newCreateTransactionRequest("qx-expired-quote", 90), nil); err == nil {
		t.Fatalf("expired quote created order: %+v", order)
	}
	// Simulate a process cache reset and re-open the earlier checkout.
	config.ResetRateCacheRuntime()
	checkout, err := GetCheckoutCounterByTradeId(first.TradeId)
	if err != nil {
		t.Fatal(err)
	}
	if checkout.ActualAmount != 11.25 || checkout.ReceiveAddress != first.ReceiveAddress {
		t.Fatalf("existing payment target changed: %+v", checkout)
	}
	owner, err := data.GetTradeIdByWalletAddressAndAmountAndToken("tron", first.ReceiveAddress, "USDT", first.ActualAmount)
	if err != nil || owner != first.TradeId {
		t.Fatalf("old payment reservation owner=%q, err=%v", owner, err)
	}
}
