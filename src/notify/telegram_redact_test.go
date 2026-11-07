package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramTransportFailureDoesNotExposeBotToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	const token = "123456:test-token-redaction"
	t.Cleanup(func() { botCacheMu.Lock(); delete(botCache, token); botCacheMu.Unlock() })
	raw, err := json.Marshal(TelegramConfig{BotToken: token, ChatID: 12345, Proxy: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = sendTelegram(string(raw), "test")
	if err == nil {
		t.Fatal("expected transport failure")
	}
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("credential not redacted: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("diagnostic detail lost: %v", err)
	}
}
