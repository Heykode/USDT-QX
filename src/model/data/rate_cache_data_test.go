package data

import (
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
)

func TestRateCacheMigrationAndValidityRoundTrip(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	// Reproduce the v2.0.0 table before the new metadata is introduced.
	if err := dao.Mdb.Migrator().DropTable(&mdb.RateCache{}); err != nil {
		t.Fatal(err)
	}
	if err := dao.Mdb.Exec(`CREATE TABLE rate_cache (
		base text PRIMARY KEY, rates text, source_url text,
		last_success_at datetime, last_attempt_at datetime,
		last_refresh_ok numeric NOT NULL DEFAULT false, last_error text,
		created_at datetime, updated_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := dao.Mdb.Exec(
		"INSERT INTO rate_cache (base,rates,source_url,last_success_at,last_attempt_at,last_refresh_ok) VALUES (?,?,?,?,?,?)",
		"cny", `{"usdt":0.125}`, "https://custom.test", now, now, true,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := dao.Mdb.AutoMigrate(&mdb.RateCache{}); err != nil {
		t.Fatal(err)
	}
	legacy, err := LoadRateCacheSnapshot("CNY")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Rates["usdt"] != 0.125 || legacy.ExpiresAt != nil || legacy.QuoteDate != "" {
		t.Fatalf("migration changed old payload or invented validity: %+v", legacy)
	}
	config.RateCacheLoad = LoadRateCacheSnapshot
	config.RateCacheLoadAll = ListRateCacheSnapshots
	config.RateCacheSave = SaveRateCacheSnapshot
	if err := SetSetting("rate", "rate.api_url", "https://custom.test", "string"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting("rate", "rate.mode", "auto", "string"); err != nil {
		t.Fatal(err)
	}
	if status := config.GetRateStatus(); len(status.Bases) != 1 || status.Bases[0].IsUsable {
		t.Fatalf("legacy payload should need refreshing: %+v", status)
	}
	for i := 1; i <= 2; i++ {
		expires := now.Add(time.Duration(i) * time.Hour)
		legacy.QuoteDate = now.Format(time.DateOnly)
		legacy.ExpiresAt = &expires
		legacy.Rates["usdt"] = float64(i) * 0.1
		if err := SaveRateCacheSnapshot(legacy); err != nil {
			t.Fatal(err)
		}
		config.ResetRateCacheRuntime()
		status := config.GetRateStatus()
		if len(status.Bases) != 1 {
			t.Fatalf("status=%+v", status)
		}
		row := status.Bases[0]
		if !row.IsUsable || row.QuoteDate != legacy.QuoteDate || row.ExpiresAt == nil || !row.ExpiresAt.Equal(expires) || row.Rates["usdt"] != legacy.Rates["usdt"] {
			t.Fatalf("metadata lost on persistence/upsert/reload: %+v", row)
		}
	}
}
