package admin

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/GMWalletApp/epusdt/middleware"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/util/authguard"
	"github.com/GMWalletApp/epusdt/util/constant"
	appjwt "github.com/GMWalletApp/epusdt/util/jwt"
	"github.com/GMWalletApp/epusdt/util/mfa"
	"github.com/labstack/echo/v4"
	"github.com/spf13/viper"
)

var loginGuard = authguard.New()

func ResetSecurityLimiterForTest() { loginGuard = authguard.New() }

func allowAdminAttempt(ctx echo.Context, account string) bool {
	retry := loginGuard.Allow(authguard.ClientIP(ctx.Request(), viper.GetString("admin_trusted_proxies")), account, time.Now())
	if retry <= 0 {
		return true
	}
	ctx.Response().Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
	_ = ctx.JSON(http.StatusTooManyRequests, map[string]any{"status_code": 429, "message": "Too many attempts. Try again later."})
	return false
}

type securityRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (c *BaseAdminController) securityUser(ctx echo.Context, req *securityRequest, factor bool) (*mdb.AdminUser, error) {
	ctx.Request().Body = http.MaxBytesReader(ctx.Response(), ctx.Request().Body, 8192)
	if ctx.Bind(req) != nil || len(req.Password) > 72 || len(req.Code) > 64 {
		return nil, constant.ParamsMarshalErr
	}
	user, err := data.GetAdminUserByID(currentAdminUserID(ctx))
	if err != nil {
		return nil, constant.SystemErr
	}
	if user.ID == 0 || user.SessionVersion != ctx.Get(middleware.AdminSessionVersionKey) {
		return nil, constant.AdminUnauthorizedErr
	}
	if !allowAdminAttempt(ctx, user.Username) {
		return nil, nil
	}
	if !data.VerifyLoginPassword(user.PasswordHash, req.Password) {
		return nil, constant.AdminInvalidCredentialErr
	}
	if factor && user.MFASecret != "" {
		if err := data.ConsumeAdminSecondFactor(user, req.Code, time.Now()); err != nil {
			return nil, constant.AdminInvalidCredentialErr
		}
	}
	return user, nil
}

func (c *BaseAdminController) SecurityStatus(ctx echo.Context) error {
	user, err := data.GetAdminUserByID(currentAdminUserID(ctx))
	if err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	return c.SucJson(ctx, map[string]any{"mfa_enabled": user.MFASecret != "", "recovery_codes_remaining": data.AdminRecoveryCodeCount(user), "username": user.Username})
}

func (c *BaseAdminController) SetupMFA(ctx echo.Context) error {
	user, err := c.securityUser(ctx, new(securityRequest), false)
	if err != nil {
		return c.FailJson(ctx, err)
	}
	if user == nil {
		return nil
	}
	if user.MFASecret != "" {
		return c.FailJson(ctx, constant.ParamsMarshalErr)
	}
	key, err := mfa.Generate(user.Username)
	if err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	encrypted, err := mfa.Seal(user.ID, key.Secret())
	if err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	expires := time.Now().Add(10 * time.Minute)
	if err = data.SaveAdminMFAPending(user, encrypted, expires); err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	img, err := key.Image(256, 256)
	if err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	return c.SucJson(ctx, map[string]any{"secret": key.Secret(), "qr_png": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), "expires_at": expires})
}

func (c *BaseAdminController) securityChanged(ctx echo.Context, user *mdb.AdminUser, codes []string) error {
	token, err := appjwt.Sign(user.ID, user.Username, user.SessionVersion)
	if err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	loginGuard.ResetAccount(user.Username)
	return c.SucJson(ctx, map[string]any{"token": token, "recovery_codes": codes})
}

func (c *BaseAdminController) EnableMFA(ctx echo.Context) error {
	req := new(securityRequest)
	user, err := c.securityUser(ctx, req, false)
	if err != nil {
		return c.FailJson(ctx, err)
	}
	if user == nil {
		return nil
	}
	codes, err := data.EnableAdminMFA(user, req.Code, time.Now())
	if err != nil {
		return c.FailJson(ctx, constant.AdminInvalidCredentialErr)
	}
	return c.securityChanged(ctx, user, codes)
}

func (c *BaseAdminController) DisableMFA(ctx echo.Context) error {
	user, err := c.securityUser(ctx, new(securityRequest), true)
	if err != nil {
		return c.FailJson(ctx, err)
	}
	if user == nil {
		return nil
	}
	if user.MFASecret == "" {
		return c.FailJson(ctx, constant.ParamsMarshalErr)
	}
	if data.DisableAdminMFA(user) != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	return c.securityChanged(ctx, user, nil)
}

func (c *BaseAdminController) RotateRecoveryCodes(ctx echo.Context) error {
	user, err := c.securityUser(ctx, new(securityRequest), true)
	if err != nil {
		return c.FailJson(ctx, err)
	}
	if user == nil {
		return nil
	}
	if user.MFASecret == "" {
		return c.FailJson(ctx, constant.ParamsMarshalErr)
	}
	codes, err := data.RotateAdminRecoveryCodes(user)
	if err != nil {
		return c.FailJson(ctx, constant.SystemErr)
	}
	return c.securityChanged(ctx, user, codes)
}
