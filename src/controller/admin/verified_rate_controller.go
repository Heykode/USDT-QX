package admin

import (
	"encoding/json"
	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/util/constant"
	"github.com/labstack/echo/v4"
	"io"
	"strings"
)

func (c *BaseAdminController) VerifiedRateStatus(ctx echo.Context) error {
	return c.SucJson(ctx, config.GetVerifiedRateStatus())
}

// Blank credential inputs preserve stored credentials; they are never echoed.
// A single setting update publishes the complete policy atomically.
func (c *BaseAdminController) SaveVerifiedRateConfig(ctx echo.Context) error {
	previous, err := config.GetVerifiedRateConfig()
	if err != nil {
		previous = config.DefaultVerifiedRateConfig()
	}
	next := previous
	decoder := json.NewDecoder(io.LimitReader(ctx.Request().Body, 8193))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&next) != nil || decoder.Decode(new(any)) != io.EOF {
		return c.FailJson(ctx, constant.SettingItemErr)
	}
	next.CMCAPIKey = strings.TrimSpace(next.CMCAPIKey)
	next.CoinGeckoAPIKey = strings.TrimSpace(next.CoinGeckoAPIKey)
	if next.CMCAPIKey == "" {
		next.CMCAPIKey = previous.CMCAPIKey
	}
	if next.CoinGeckoAPIKey == "" {
		next.CoinGeckoAPIKey = previous.CoinGeckoAPIKey
	}
	if config.ValidateVerifiedRateConfig(next) != nil {
		return c.FailJson(ctx, constant.SettingItemErr)
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return c.FailJson(ctx, constant.SettingItemErr)
	}
	if err = data.SetSetting("rate", config.VerifiedRateSettingKey, string(raw), "json"); err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	return c.SucJson(ctx, config.GetVerifiedRateStatus())
}
func (c *BaseAdminController) RefreshVerifiedRate(ctx echo.Context) error {
	return c.SucJson(ctx, config.RefreshVerifiedRate())
}
