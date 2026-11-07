package data

import (
	"encoding/json"
	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/util/mfa"
	"github.com/pquerna/otp/totp"
	"github.com/spf13/viper"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminFactorConcurrentConsumption(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	viper.Set("admin_mfa_key_file", filepath.Join(t.TempDir(), "mfa.key"))
	user := &mdb.AdminUser{Username: "mfa-admin", Status: mdb.AdminUserStatusEnable, PasswordHash: "test"}
	if err := dao.Mdb.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	key, _ := mfa.Generate(user.Username)
	encrypted, err := mfa.Seal(user.ID, key.Secret())
	if err != nil {
		t.Fatal(err)
	}
	codes, hashes, _ := mfa.RecoveryCodes()
	raw, _ := json.Marshal(hashes)
	dao.Mdb.Model(user).Updates(map[string]any{"mfa_secret": encrypted, "mfa_recovery_hashes": string(raw)})
	now := time.Now()
	code, _ := totp.GenerateCode(key.Secret(), now)
	for _, factor := range []string{code, codes[0]} {
		user, _ = GetAdminUserByID(user.ID)
		var successes atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			copy := *user
			wg.Add(1)
			go func() {
				defer wg.Done()
				if ConsumeAdminSecondFactor(&copy, factor, now) == nil {
					successes.Add(1)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("second factor accepted %d times", successes.Load())
		}
	}
	stale := *user
	if err := RevokeAdminSessions(user); err != nil {
		t.Fatal(err)
	}
	if ConsumeAdminSecondFactor(&stale, codes[1], now) == nil {
		t.Fatal("factor consumed with revoked session")
	}
}
