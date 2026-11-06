package route

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	apphttp "github.com/GMWalletApp/epusdt/util/http"
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

func TestQXEPayQueryEnforcesAuthenticationAndReturnsAllStatuses(t *testing.T) {
	e := setupTestEnv(t)
	apiKey, err := data.GetEnabledApiKey("1")
	if err != nil || apiKey == nil || apiKey.ID == 0 {
		t.Fatalf("load merchant: %v", err)
	}
	order := &mdb.Orders{TradeId: "qx-query-trade", OrderId: "qx-query-order", Amount: 90, Currency: "CNY", Status: mdb.StatusWaitPay, PaymentType: mdb.PaymentTypeEpay, EpayType: "USDT.tron", ApiKeyID: apiKey.ID}
	if err := dao.Mdb.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	// Include the actual SPA middleware. Backend query routes must win even
	// when a same-named file is accidentally included in the frontend bundle.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "api.php"), []byte("frontend file"), 0600); err != nil {
		t.Fatal(err)
	}
	e.Use(echoMiddleware.StaticWithConfig(echoMiddleware.StaticConfig{
		Root: root, HTML5: true,
		Skipper: func(c echo.Context) bool { return apphttp.ShouldSkipSPAFallback(c.Request().URL.Path) },
	}))
	for _, path := range []string{"/api.php", "/payments/epay/v1/order/create-transaction/api.php"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, status := range []int{mdb.StatusWaitPay, mdb.StatusWaitSelect, mdb.StatusExpired, mdb.StatusPaySuccess} {
				if err := dao.Mdb.Model(order).Update("status", status).Error; err != nil {
					t.Fatal(err)
				}
				values := url.Values{"act": {"order"}, "pid": {"1"}, "key": {testAPIToken}, "out_trade_no": {order.OrderId}}
				req := httptest.NewRequest(method, path+"?"+values.Encode(), nil)
				if method == http.MethodPost {
					req = httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				wantStatus := float64(0)
				if status == mdb.StatusPaySuccess {
					wantStatus = 1
				}
				resp := parseResp(t, rec)
				if resp["code"] != float64(1) || resp["status"] != wantStatus || resp["money"] != "90.00" || resp["type"] != "USDT.tron" {
					t.Fatalf("%s %s status %d: %+v", method, path, status, resp)
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("merchant response can be cached")
				}
			}
		}
	}
	for _, scenario := range []string{"disabled", "ip-blocked", "wrong-key", "conflicting-ids"} {
		t.Run(scenario, func(t *testing.T) {
			updates := map[string]interface{}{"status": mdb.ApiKeyStatusEnable, "ip_whitelist": ""}
			values := url.Values{"act": {"order"}, "pid": {"1"}, "key": {testAPIToken}, "trade_no": {order.TradeId}}
			switch scenario {
			case "disabled":
				updates["status"] = mdb.ApiKeyStatusDisable
			case "ip-blocked":
				updates["ip_whitelist"] = "203.0.113.99"
			case "wrong-key":
				values.Set("key", "incorrect")
			case "conflicting-ids":
				values.Set("out_trade_no", "different-order")
			}
			if err := dao.Mdb.Model(apiKey).Updates(updates).Error; err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api.php", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "192.0.2.12:1234"
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if resp := parseResp(t, rec); resp["code"] != float64(0) || resp["trade_no"] != nil {
				t.Fatalf("rejected query leaked order: %+v", resp)
			}
		})
	}
}
