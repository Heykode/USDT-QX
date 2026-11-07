package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/model/data"
)

func TestVerifiedRateAdminConfigProtectsCredentials(t *testing.T) {
	e, token := setupAdminTestEnv(t)
	path := "/admin/api/v1/settings/rate/verified"
	request := func(method, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	if r := request(http.MethodGet, "", ""); r.Code != 401 {
		t.Fatalf("unauthenticated status=%d", r.Code)
	}
	cfg := config.DefaultVerifiedRateConfig()
	cfg.Enabled = true
	cfg.CMCAPIKey = "test-cmc-secret"
	cfg.CoinGeckoAPIKey = "test-cg-secret"
	cfg.ReferencePrice = 7
	cfg.MinPrice = 6
	cfg.MaxPrice = 8
	b, _ := json.Marshal(cfg)
	r := request(http.MethodPut, string(b), token)
	if r.Code != 200 {
		t.Fatalf("save status=%d body=%s", r.Code, r.Body.String())
	}
	for _, rec := range []*httptest.ResponseRecorder{r, request(http.MethodGet, "", token), doGetAdmin(e, "/admin/api/v1/settings?group=rate", token), doGetAdmin(e, "/admin/api/v1/settings/rate/status", token)} {
		if strings.Contains(rec.Body.String(), "test-cmc-secret") || strings.Contains(rec.Body.String(), "test-cg-secret") {
			t.Fatal("API exposed keys")
		}
	}
	if rec := request(http.MethodPut, `{"cmc_api_key":"","coingecko_api_key":"","max_spread_percent":0.8}`, token); rec.Code != 200 {
		t.Fatal("update failed")
	}
	stored, _ := config.GetVerifiedRateConfig()
	if stored.CMCAPIKey != cfg.CMCAPIKey || stored.CoinGeckoAPIKey != cfg.CoinGeckoAPIKey || stored.MaxSpreadPercent != .8 {
		t.Fatal("blank fields overwrote credentials")
	}
	old := data.GetSettingString(config.VerifiedRateSettingKey, "")
	if rec := request(http.MethodPut, `{"max_spread_percent":0}`, token); rec.Code == 200 {
		t.Fatal("invalid policy accepted")
	}
	if data.GetSettingString(config.VerifiedRateSettingKey, "") != old {
		t.Fatal("invalid update modified live config")
	}
}
