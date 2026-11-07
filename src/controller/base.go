package controller

import (
	"github.com/GMWalletApp/epusdt/util/constant"
	"github.com/GMWalletApp/epusdt/util/http"
	"github.com/gookit/validate"
	"github.com/gookit/validate/locales/zhcn"
	"github.com/gookit/validate/locales/zhtw"
	"github.com/labstack/echo/v4"
)

var Ctrl = &BaseController{}

// Controllers are shared by concurrent requests; keep request state local.
type BaseController struct {
	http.Resp
}

func (c *BaseController) GetLocale(ctx echo.Context) string {
	return ctx.Request().Header.Get("locale")
}

func (c *BaseController) ValidateStruct(ctx echo.Context, i interface{}, scene ...string) error {
	v := validate.Struct(i, scene...)
	switch c.GetLocale(ctx) {
	case "zh-tw":
		zhtw.Register(v)
	default:
		zhcn.Register(v)
	}
	if v.Validate() {
		return nil
	}
	return constant.ParamsMarshalErr
}
