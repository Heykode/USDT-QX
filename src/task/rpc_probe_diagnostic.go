package task

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/rpc"
)

const (
	rpcProbeStageConnection      = "connection"
	rpcProbeStageBlockHeader     = "block_header"
	rpcProbeStageSubscription    = "subscription"
	rpcProbeStageHistoricalQuery = "historical_query"
	rpcProbeStageConfiguration   = "configuration"
)

// RpcProbeDiagnostic is safe for an authenticated API response and operational
// logs. Stage and Category are fixed values; no provider-controlled strings,
// error data, URL components or credentials are retained here. Classification
// is diagnostic only and never changes node selection or the probe verdict.
type RpcProbeDiagnostic struct {
	Stage      string `json:"stage" example:"subscription"`
	Category   string `json:"category" example:"rate_limited"`
	HTTPStatus int    `json:"http_status,omitempty" example:"429"`
	RPCCode    *int   `json:"rpc_code,omitempty" example:"-32601"`
}

type rpcProbeError struct {
	stage string
	cause error
}

// Even an accidental formatting of this wrapper must not reveal its cause.
func (e *rpcProbeError) Error() string { return "RPC probe failed at " + e.stage }
func (e *rpcProbeError) Unwrap() error { return e.cause }

func rpcProbeDiagnostic(err error) *RpcProbeDiagnostic {
	if err == nil {
		return nil
	}
	diagnostic := &RpcProbeDiagnostic{Stage: "unknown", Category: "provider_error"}
	var probeErr *rpcProbeError
	if errors.As(err, &probeErr) {
		diagnostic.Stage = probeErr.stage
		err = probeErr.cause
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		code := rpcErr.ErrorCode()
		diagnostic.RPCCode = &code
	}
	var httpErr rpc.HTTPError
	if errors.As(err, &httpErr) {
		diagnostic.HTTPStatus = httpErr.StatusCode
	}

	var networkErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkErr) && networkErr.Timeout():
		diagnostic.Category = "timeout"
	case errors.Is(err, context.Canceled):
		diagnostic.Category = "canceled"
	case diagnostic.HTTPStatus == http.StatusTooManyRequests:
		diagnostic.Category = "rate_limited"
	case diagnostic.HTTPStatus == http.StatusUnauthorized || diagnostic.HTTPStatus == http.StatusForbidden:
		diagnostic.Category = "access_denied"
	case diagnostic.HTTPStatus == http.StatusRequestTimeout || diagnostic.HTTPStatus == http.StatusGatewayTimeout:
		diagnostic.Category = "timeout"
	case diagnostic.HTTPStatus >= 500:
		diagnostic.Category = "service_unavailable"
	case diagnostic.HTTPStatus > 0:
		diagnostic.Category = "request_rejected"
	case diagnostic.Stage == rpcProbeStageConfiguration:
		diagnostic.Category = "configuration_error"
	case errors.Is(err, rpc.ErrNotificationsUnsupported):
		diagnostic.Category = "unsupported"
	case rpcErr != nil:
		diagnostic.Category = rpcProbeRPCErrorCategory(rpcErr)
	case errors.As(err, &networkErr) || diagnostic.Stage == rpcProbeStageConnection:
		diagnostic.Category = "connection_error"
	}
	return diagnostic
}

func rpcProbeRPCErrorCategory(err rpc.Error) string {
	if err.ErrorCode() == -32601 {
		return "unsupported"
	}
	// Provider messages are inspected only for classification. Their contents
	// must never be copied into diagnostics, even when the message echoes a key.
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "timeout") || strings.Contains(message, "timed out") {
		return "timeout"
	}
	for _, fragment := range []string{"rate limit", "too many requests", "quota", "compute unit", "credit", "requests per"} {
		if strings.Contains(message, fragment) {
			return "rate_limited"
		}
	}
	if isEvmLogRangeLimitError(err) {
		return "range_limit"
	}
	for _, fragment := range []string{"pruned history", "historical state unavailable", "missing trie node", "archive"} {
		if strings.Contains(message, fragment) {
			return "history_unavailable"
		}
	}
	for _, fragment := range []string{"not supported", "unsupported", "method not found", "method is disabled", "eth_getlogs disabled"} {
		if strings.Contains(message, fragment) {
			return "unsupported"
		}
	}
	if err.ErrorCode() == -32602 || err.ErrorCode() == -32600 {
		return "invalid_request"
	}
	return "provider_error"
}
