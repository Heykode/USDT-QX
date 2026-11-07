package telegram

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	tb "gopkg.in/telebot.v3"
)

func TestWalletButtonsRequireConfiguredOperator(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":4242,"type":"private"}}}`)
	}))
	defer server.Close()
	bot, err := tb.NewBot(tb.Settings{Token: "test-token", Offline: true, Synchronous: true, URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	oldBot, oldAdmin := bots, adminChatID
	t.Cleanup(func() { bots, adminChatID = oldBot, oldAdmin })
	bots, adminChatID = bot, 4242
	RegisterHandle()
	wallet, err := data.AddWalletAddress("wallet_authorization")
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(wallet.ID)
	// Register the real dynamically-created buttons from the wallet menu.
	ctx := bot.NewContext(tb.Update{Callback: &tb.Callback{Data: id, Sender: &tb.User{ID: 4242}, Message: &tb.Message{ID: 1, Chat: &tb.Chat{ID: 4242}}}})
	if err := WalletInfo(ctx); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int64{9999, 0} {
		for _, button := range []string{"disableBtn", "delBtn"} {
			var sender *tb.User
			if actor != 0 {
				sender = &tb.User{ID: actor}
			}
			bot.ProcessUpdate(tb.Update{Callback: &tb.Callback{ID: "test", Data: "\f" + button + "|" + id, Sender: sender, Message: &tb.Message{ID: 1, Chat: &tb.Chat{ID: 4242}}}})
			got, err := data.GetWalletAddressById(wallet.ID)
			if err != nil || got.ID == 0 || got.Status != mdb.TokenStatusEnable {
				t.Fatalf("unauthorized actor %d changed wallet via %s: %+v err=%v", actor, button, got, err)
			}
		}
	}
	for _, action := range []struct {
		button string
		status int64
	}{{"disableBtn", mdb.TokenStatusDisable}, {"enableBtn", mdb.TokenStatusEnable}} {
		bot.ProcessUpdate(tb.Update{Callback: &tb.Callback{ID: "test", Data: "\f" + action.button + "|" + id, Sender: &tb.User{ID: 4242}, Message: &tb.Message{ID: 1, Chat: &tb.Chat{ID: 4242}}}})
		got, err := data.GetWalletAddressById(wallet.ID)
		if err != nil || got.Status != action.status {
			t.Fatalf("operator action %s failed: %+v err=%v", action.button, got, err)
		}
	}
}
