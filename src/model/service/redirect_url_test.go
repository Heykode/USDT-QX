package service

import (
	"fmt"
	"testing"

	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/util/constant"
)

func TestCheckoutRejectsUnsafeRedirectTargets(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	unsafe := []string{
		"javascript:alert(1)", "JavaScript://merchant.example/%0aalert(1)",
		"data:text/html,test", "vbscript:msgbox(1)", "ftp://merchant.example/return",
		"//merchant.example/return", "/relative", "https://", "https://:443/return",
		"https://merchant.example/\nreturn", "https:\\merchant.example/return",
	}
	for i, raw := range unsafe {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			req := newCreateTransactionRequest(fmt.Sprintf("unsafe_redirect_%d", i), 5)
			req.Token, req.Network, req.RedirectUrl = "", "", raw
			if _, err := CreateTransaction(req, nil); err != constant.OrderRedirectURLErr {
				t.Errorf("create with %q: %v, want redirect error", raw, err)
			}
			for _, paymentType := range []string{mdb.PaymentTypeGmpay, mdb.PaymentTypeEpay} {
				order := &mdb.Orders{TradeId: "legacy", PaymentType: paymentType, RedirectUrl: raw}
				if got := buildCheckoutResponse(order).RedirectUrl; got != "" {
					t.Errorf("legacy %s exposed unsafe target %q", paymentType, got)
				}
			}
			if _, err := appendQueryParams(raw, map[string]string{"trade_no": "test"}); err != constant.OrderRedirectURLErr {
				t.Errorf("return hop accepted %q: %v", raw, err)
			}
		})
	}
	var count int64
	if err := dao.Mdb.Model(&mdb.Orders{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unsafe requests persisted orders: count=%d err=%v", count, err)
	}
}

func TestCheckoutPreservesValidRedirectTargets(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	for i, raw := range []string{"", "https://merchant.example/return?from=wallet#done", "http://merchant.example:8080/return", "HTTPS://Merchant.Example/Return"} {
		req := newCreateTransactionRequest(fmt.Sprintf("valid_redirect_%d", i), 5)
		req.Token, req.Network, req.RedirectUrl = "", "", raw
		created, err := CreateTransaction(req, nil)
		if err != nil {
			t.Fatal(err)
		}
		order, err := data.GetOrderInfoByTradeId(created.TradeId)
		if err != nil {
			t.Fatal(err)
		}
		if got := buildCheckoutResponse(order).RedirectUrl; got != raw {
			t.Fatalf("redirect changed: got %q want %q", got, raw)
		}
	}
}
