package route

import (
	"encoding/json"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/pquerna/otp/totp"
	"strings"
	"testing"
	"time"
)

func TestAdminMFAEnrollmentLoginRecoveryAndRevocation(t *testing.T) {
	e, oldToken := setupAdminTestEnv(t)
	path := "/admin/api/v1/auth/"
	setup := assertOK(t, doPostAdmin(e, path+"mfa/setup", map[string]any{"password": testAdminPassword}, oldToken))["data"].(map[string]any)
	secret := setup["secret"].(string)
	user, _ := data.GetAdminUserByUsername(testAdminUsername)
	if user.MFASecret != "" || strings.Contains(user.MFAPendingSecret, secret) {
		t.Fatal("enrollment enabled before verification or stored plaintext")
	}
	if rec := doPostAdmin(e, path+"mfa/enable", map[string]any{"password": testAdminPassword, "code": "bad"}, oldToken); rec.Code == 200 {
		t.Fatal("bad enrollment accepted")
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	enabled := assertOK(t, doPostAdmin(e, path+"mfa/enable", map[string]any{"password": testAdminPassword, "code": code}, oldToken))["data"].(map[string]any)
	token := enabled["token"].(string)
	codes := enabled["recovery_codes"].([]any)
	if len(codes) != 10 {
		t.Fatal("missing recovery codes")
	}
	if rec := doGetAdmin(e, path+"me", oldToken); rec.Code != 401 {
		t.Fatal("old session survived enable")
	}
	user, _ = data.GetAdminUserByUsername(testAdminUsername)
	if strings.Contains(user.MFARecoveryHashes, codes[0].(string)) {
		t.Fatal("plaintext recovery code stored")
	}
	for _, endpoint := range []string{"me", "security"} {
		body := doGetAdmin(e, path+endpoint, token).Body.String()
		if strings.Contains(body, secret) || strings.Contains(body, user.MFASecret) || strings.Contains(body, user.MFARecoveryHashes) {
			t.Fatal("security material leaked")
		}
	}
	login := map[string]any{"username": testAdminUsername, "password": testAdminPassword}
	challenge := assertOK(t, doPost(e, path+"login", login))["data"].(map[string]any)
	if challenge["mfa_required"] != true || challenge["token"] != nil {
		t.Fatal("password alone issued token")
	}
	login["code"] = code
	if rec := doPost(e, path+"login", login); rec.Code == 200 {
		t.Fatal("enrollment code replay accepted")
	}
	login["code"] = codes[0]
	loginData := assertOK(t, doPost(e, path+"login", login))["data"].(map[string]any)
	if loginData["token"] == nil {
		t.Fatal("recovery login failed")
	}
	if rec := doPost(e, path+"login", login); rec.Code == 200 {
		t.Fatal("recovery replay accepted")
	}
	if rec := doPostAdmin(e, path+"mfa/disable", map[string]any{"password": testAdminPassword}, token); rec.Code == 200 {
		t.Fatal("disabled MFA without second factor")
	}
	rotated := assertOK(t, doPostAdmin(e, path+"mfa/recovery-codes", map[string]any{"password": testAdminPassword, "code": codes[1]}, token))["data"].(map[string]any)
	if rec := doGetAdmin(e, path+"me", token); rec.Code != 401 {
		t.Fatal("old session survived recovery rotation")
	}
	token = rotated["token"].(string)
	newCodes := rotated["recovery_codes"].([]any)
	login["code"] = codes[2]
	if rec := doPost(e, path+"login", login); rec.Code == 200 {
		t.Fatal("old recovery set still valid")
	}
	disabled := assertOK(t, doPostAdmin(e, path+"mfa/disable", map[string]any{"password": testAdminPassword, "code": newCodes[0]}, token))["data"].(map[string]any)
	if rec := doGetAdmin(e, path+"me", token); rec.Code != 401 {
		t.Fatal("old session survived disable")
	}
	token = disabled["token"].(string)
	assertOK(t, doPostAdmin(e, path+"logout", map[string]any{}, token))
	if rec := doGetAdmin(e, path+"me", token); rec.Code != 401 {
		t.Fatal("logout did not revoke")
	}
	delete(login, "code")
	assertOK(t, doPost(e, path+"login", login))
}

func TestAdminLoginLimitsAndSecuritySettingProtection(t *testing.T) {
	e, token := setupAdminTestEnv(t)
	for i := 0; i < 12; i++ {
		doPost(e, "/admin/api/v1/auth/login", map[string]any{"username": testAdminUsername, "password": "wrong"})
	}
	rec := doPost(e, "/admin/api/v1/auth/login", map[string]any{"username": testAdminUsername, "password": testAdminPassword})
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatal("login not limited", rec.Code)
	}
	for _, key := range []string{mdb.SettingKeyJwtSecret, mdb.SettingKeyInitAdminPasswordHash, strings.ToUpper(mdb.SettingKeyJwtSecret)} {
		before := data.GetSettingString(key, "")
		doPutAdmin(e, "/admin/api/v1/settings", map[string]any{"items": []any{map[string]any{"group": "system", "key": key, "value": "attacker", "type": "string"}}}, token)
		if data.GetSettingString(key, "") != before {
			t.Fatal("credential overwritten")
		}
		if rec := doDeleteAdmin(e, "/admin/api/v1/settings/"+key, token); rec.Code == 200 {
			t.Fatal("credential deleted")
		}
	}
	user, _ := data.GetAdminUserByUsername(testAdminUsername)
	dao.Mdb.Model(&mdb.AdminUser{}).Where("id = ?", user.ID).Update("status", mdb.AdminUserStatusDisable)
	if rec := doGetAdmin(e, "/admin/api/v1/auth/me", token); rec.Code != 401 {
		t.Fatal("disabled user session accepted")
	}
}

func TestAdminMFAExpiredEnrollment(t *testing.T) {
	e, token := setupAdminTestEnv(t)
	setup := assertOK(t, doPostAdmin(e, "/admin/api/v1/auth/mfa/setup", map[string]any{"password": testAdminPassword}, token))["data"].(map[string]any)
	user, _ := data.GetAdminUserByUsername(testAdminUsername)
	dao.Mdb.Model(&mdb.AdminUser{}).Where("id = ?", user.ID).Update("mfa_pending_expires_at", time.Now().Add(-time.Second).Unix())
	code, _ := totp.GenerateCode(setup["secret"].(string), time.Now())
	rec := doPostAdmin(e, "/admin/api/v1/auth/mfa/enable", map[string]any{"password": testAdminPassword, "code": code}, token)
	if rec.Code == 200 {
		t.Fatal("expired enrollment accepted")
	}
	b, _ := json.Marshal(assertOK(t, doGetAdmin(e, "/admin/api/v1/auth/security", token))["data"])
	if !strings.Contains(string(b), `"mfa_enabled":false`) {
		t.Fatal("expiry enabled MFA")
	}
}
