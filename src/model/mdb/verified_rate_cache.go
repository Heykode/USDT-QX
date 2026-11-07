package mdb

import "time"

// Separate from legacy daily-rate caches: neither engine can relabel the other's quotes.
type VerifiedRateCache struct {
	Base      string    `gorm:"column:base;primaryKey;size:16"`
	State     string    `gorm:"column:state;type:text;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (*VerifiedRateCache) TableName() string { return "verified_rate_cache" }
