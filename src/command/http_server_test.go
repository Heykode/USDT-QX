package command

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/labstack/echo/v4"
)

func TestAccessLogDoesNotPersistEPayQuerySecret(t *testing.T) {
	previous := config.HTTPAccessLog
	config.HTTPAccessLog = true
	t.Cleanup(func() { config.HTTPAccessLog = previous })
	e := echo.New()
	var output bytes.Buffer
	e.Logger.SetOutput(&output)
	MiddlewareRegister(e)
	e.GET("/api.php", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api.php?pid=1&key=do-not-log-this-secret", nil))
	if got := output.String(); !strings.Contains(got, "/api.php") || strings.Contains(got, "do-not-log-this-secret") || strings.Contains(got, "key=") {
		t.Fatalf("access log must contain path but no query credentials: %q", got)
	}
}
