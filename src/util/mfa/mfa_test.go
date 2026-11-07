package mfa

import (
	"github.com/pquerna/otp/totp"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncryptionBoundToAccountAndMissingKeyFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mfa.key")
	viper.Set("admin_mfa_key_file", p)
	t.Cleanup(func() { viper.Set("admin_mfa_key_file", "") })
	key, err := Generate("admin")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := Seal(1, key.Secret())
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == key.Secret() {
		t.Fatal("plaintext stored")
	}
	if secret, err := Open(1, encrypted); err != nil || secret != key.Secret() {
		t.Fatal("cannot decrypt", err)
	}
	if _, err := Open(2, encrypted); err == nil {
		t.Fatal("account substitution accepted")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(1, encrypted); err == nil {
		t.Fatal("missing encryption key ignored")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("missing key was silently recreated")
	}
}
func TestTimeWindowAndReplay(t *testing.T) {
	key, _ := Generate("admin")
	now := time.Unix(1800000000, 0)
	code, _ := totp.GenerateCode(key.Secret(), now)
	step, err := Match(key.Secret(), code, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Match(key.Secret(), code, step, now); err == nil {
		t.Fatal("replayed code accepted")
	}
	if _, err := Match(key.Secret(), code, 0, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired code accepted")
	}
}
