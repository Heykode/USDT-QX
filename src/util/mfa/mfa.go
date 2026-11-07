// Package mfa provides standard TOTP and encrypted storage for administrator factors.
package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/spf13/viper"
)

var ErrInvalid = errors.New("invalid or already used verification code")
var keyMu sync.Mutex

func Generate(account string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{Issuer: "USDT-QX", AccountName: account, Period: 30, SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
}

// Match returns the exact accepted time step so the database can reject replay.
func Match(secret, code string, last int64, now time.Time) (int64, error) {
	if len(code) != 6 {
		return 0, ErrInvalid
	}
	for _, offset := range []int64{0, -1, 1} {
		step := now.Unix()/30 + offset
		if step <= last {
			continue
		}
		expected, err := totp.GenerateCode(secret, time.Unix(step*30, 0))
		if err != nil {
			return 0, err
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return step, nil
		}
	}
	return 0, ErrInvalid
}

func RecoveryCodes() (codes, hashes []string, err error) {
	for i := 0; i < 10; i++ {
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			return nil, nil, err
		}
		code := hex.EncodeToString(b)
		codes = append(codes, code)
		hashes = append(hashes, RecoveryHash(code))
	}
	return
}

func RecoveryHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

// The installation key is outside the database. Keep it when backing up or
// moving the service. Missing keys must never be recreated while decrypting.
func encryptionKey(create bool) ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	path := viper.GetString("admin_mfa_key_file")
	if path == "" {
		root := config.RuntimePath
		if root == "" {
			root = viper.GetString("runtime_root_path")
		}
		if root == "" {
			return nil, errors.New("MFA key location is not configured")
		}
		path = filepath.Join(root, "admin-mfa.key")
	}
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			b := make([]byte, 32)
			if _, err = rand.Read(b); err == nil {
				_, err = f.Write(b)
			}
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		} else if !os.IsExist(err) {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("MFA key must be a private regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) != 32 {
		return nil, errors.New("invalid MFA encryption key")
	}
	return b, nil
}

func aead(create bool) (cipher.AEAD, error) {
	key, err := encryptionKey(create)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func Seal(userID uint64, secret string) (string, error) {
	a, err := aead(true)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := a.Seal(nonce, nonce, []byte(secret), []byte(fmt.Sprintf("USDT-QX/admin/%d/mfa", userID)))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func Open(userID uint64, encrypted string) (string, error) {
	a, err := aead(false)
	if err != nil {
		return "", err
	}
	b, err := base64.RawStdEncoding.DecodeString(encrypted)
	if err != nil || len(b) < a.NonceSize() {
		return "", errors.New("invalid encrypted MFA secret")
	}
	plain, err := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(fmt.Sprintf("USDT-QX/admin/%d/mfa", userID)))
	return string(plain), err
}
