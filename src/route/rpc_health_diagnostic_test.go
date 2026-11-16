package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
)

func TestRpcHealthCheckResponseIncludesOnlySafeOptionalDiagnostic(t *testing.T) {
	e, token := setupAdminTestEnv(t)
	var reject atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reject.Load() {
			http.Error(w, "provider-echoed-secret "+r.URL.String(), http.StatusTooManyRequests)
			return
		}
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode probe request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": "0x100"})
	}))
	defer server.Close()
	node := mdb.RpcNode{Network: mdb.NetworkBsc, Type: mdb.RpcNodeTypeHttp,
		Url:     strings.Replace(server.URL, "http://", "http://user-secret:password-secret@", 1) + "/path-secret?key=query-secret",
		Enabled: true, Status: mdb.RpcNodeStatusUnknown}
	if err := dao.Mdb.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	path := "/admin/api/v1/rpc-nodes/" + strconv.FormatUint(node.ID, 10) + "/health-check"
	for _, failed := range []bool{false, true} {
		reject.Store(failed)
		rec := doPostAdmin(e, path, nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("health check status = %d, body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "http://") {
			t.Fatalf("health check leaked credentials/provider message: %s", rec.Body.String())
		}
		var envelope struct {
			Data map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if !failed {
			if envelope.Data["status"] != "ok" || envelope.Data["last_latency_ms"].(float64) < 0 {
				t.Fatalf("healthy response changed: %s", rec.Body.String())
			}
			if _, found := envelope.Data["diagnostic"]; found {
				t.Fatalf("healthy response should omit diagnostic: %s", rec.Body.String())
			}
			continue
		}
		if envelope.Data["status"] != "down" || envelope.Data["last_latency_ms"] != float64(-1) {
			t.Fatalf("failed response changed: %s", rec.Body.String())
		}
		diagnostic, ok := envelope.Data["diagnostic"].(map[string]interface{})
		if !ok || diagnostic["category"] != "rate_limited" || diagnostic["stage"] != "block_header" || diagnostic["http_status"] != float64(429) {
			t.Fatalf("missing safe diagnostic: %s", rec.Body.String())
		}
	}
}
