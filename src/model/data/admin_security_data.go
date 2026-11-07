package data

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/util/mfa"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func privateAdminDB() *gorm.DB {
	return dao.Mdb.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
}

func adminCAS(user *mdb.AdminUser) *gorm.DB {
	return privateAdminDB().Model(&mdb.AdminUser{}).Where("id = ? AND session_version = ? AND status = ? AND password_hash = ? AND mfa_secret = ?", user.ID, user.SessionVersion, mdb.AdminUserStatusEnable, user.PasswordHash, user.MFASecret)
}

func checkedAdminUpdate(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("admin security state changed")
	}
	return nil
}

func RevokeAdminSessions(user *mdb.AdminUser) error {
	return checkedAdminUpdate(adminCAS(user).Updates(map[string]any{"session_version": gorm.Expr("session_version + 1"), "mfa_pending_secret": "", "mfa_pending_expires_at": 0}))
}

func SaveAdminMFAPending(user *mdb.AdminUser, encrypted string, expires time.Time) error {
	if user.MFASecret != "" {
		return errors.New("MFA already enabled")
	}
	return checkedAdminUpdate(adminCAS(user).Updates(map[string]any{"mfa_pending_secret": encrypted, "mfa_pending_expires_at": expires.Unix()}))
}

func EnableAdminMFA(user *mdb.AdminUser, code string, now time.Time) ([]string, error) {
	if user.MFASecret != "" || user.MFAPendingSecret == "" || now.Unix() >= user.MFAPendingExpiresAt {
		return nil, mfa.ErrInvalid
	}
	secret, err := mfa.Open(user.ID, user.MFAPendingSecret)
	if err != nil {
		return nil, err
	}
	step, err := mfa.Match(secret, code, 0, now)
	if err != nil {
		return nil, err
	}
	codes, hashes, err := mfa.RecoveryCodes()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(hashes)
	err = checkedAdminUpdate(adminCAS(user).Where("mfa_pending_secret = ? AND mfa_pending_expires_at = ?", user.MFAPendingSecret, user.MFAPendingExpiresAt).Updates(map[string]any{
		"mfa_secret": user.MFAPendingSecret, "mfa_pending_secret": "", "mfa_pending_expires_at": 0,
		"mfa_last_step": step, "mfa_recovery_hashes": string(raw), "session_version": gorm.Expr("session_version + 1"),
	}))
	if err != nil {
		return nil, err
	}
	user.SessionVersion++
	return codes, nil
}

// This atomic compare-and-update consumes a TOTP time step or a recovery code
// once, including concurrent requests and requests on another server process.
func ConsumeAdminSecondFactor(user *mdb.AdminUser, code string, now time.Time) error {
	if user.MFASecret == "" {
		return nil
	}
	code = strings.TrimSpace(code)
	if len(code) == 6 {
		secret, err := mfa.Open(user.ID, user.MFASecret)
		if err != nil {
			return err
		}
		step, err := mfa.Match(secret, code, user.MFALastStep, now)
		if err != nil {
			return err
		}
		err = checkedAdminUpdate(adminCAS(user).Where("mfa_last_step = ?", user.MFALastStep).Update("mfa_last_step", step))
		if err == nil {
			user.MFALastStep = step
		}
		return err
	}
	if len(code) != 32 {
		return mfa.ErrInvalid
	}
	var hashes []string
	if json.Unmarshal([]byte(user.MFARecoveryHashes), &hashes) != nil {
		return mfa.ErrInvalid
	}
	hash := mfa.RecoveryHash(code)
	for i, candidate := range hashes {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(candidate)) != 1 {
			continue
		}
		hashes = append(hashes[:i], hashes[i+1:]...)
		raw, _ := json.Marshal(hashes)
		err := checkedAdminUpdate(adminCAS(user).Where("mfa_recovery_hashes = ?", user.MFARecoveryHashes).Update("mfa_recovery_hashes", string(raw)))
		if err == nil {
			user.MFARecoveryHashes = string(raw)
		}
		return err
	}
	return mfa.ErrInvalid
}

func DisableAdminMFA(user *mdb.AdminUser) error {
	err := checkedAdminUpdate(adminCAS(user).Updates(map[string]any{
		"mfa_secret": "", "mfa_pending_secret": "", "mfa_pending_expires_at": 0,
		"mfa_last_step": 0, "mfa_recovery_hashes": "", "session_version": gorm.Expr("session_version + 1"),
	}))
	if err == nil {
		user.SessionVersion++
	}
	return err
}

func RotateAdminRecoveryCodes(user *mdb.AdminUser) ([]string, error) {
	codes, hashes, err := mfa.RecoveryCodes()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(hashes)
	err = checkedAdminUpdate(adminCAS(user).Where("mfa_recovery_hashes = ?", user.MFARecoveryHashes).Updates(map[string]any{"mfa_recovery_hashes": string(raw), "session_version": gorm.Expr("session_version + 1")}))
	if err != nil {
		return nil, err
	}
	user.SessionVersion++
	return codes, nil
}

func AdminRecoveryCodeCount(user *mdb.AdminUser) int {
	var hashes []string
	_ = json.Unmarshal([]byte(user.MFARecoveryHashes), &hashes)
	return len(hashes)
}
