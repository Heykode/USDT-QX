package task

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Query the same starting block with a smaller upper bound when the provider
// explicitly rejects the range or result size. The returned end is the only
// range the caller may mark as scanned. Health checks use this same policy.
func filterEvmLogsWithRangeLimit(ctx context.Context, client *ethclient.Client, query ethereum.FilterQuery) ([]types.Log, int64, error) {
	from, to := query.FromBlock.Int64(), query.ToBlock.Int64()
	for {
		if err := ctx.Err(); err != nil {
			return nil, to, err
		}
		query.ToBlock = big.NewInt(to)
		logs, err := client.FilterLogs(ctx, query)
		if err == nil {
			return logs, to, nil
		}
		if to <= from || !isEvmLogRangeLimitError(err) {
			return nil, to, err
		}
		to = from + (to-from+1)/2 - 1
	}
}

func isEvmLogRangeLimitError(err error) bool {
	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	switch rpcErr.ErrorCode() {
	case -32000, -32005, -32602:
	default:
		return false
	}
	message := strings.ToLower(err.Error())
	// Some providers use the same RPC code for rate and range limits. Retrying
	// quota failures with smaller batches would increase load without progress.
	for _, quota := range []string{"rate limit", "too many requests", "quota", "credit", "compute unit", "requests per"} {
		if strings.Contains(message, quota) {
			return false
		}
	}
	limited := strings.Contains(message, "limit") || strings.Contains(message, "exceed") || strings.Contains(message, "maximum") || strings.Contains(message, "too wide")
	return (strings.Contains(message, "range") && limited) ||
		(strings.Contains(message, "query returned more than") && strings.Contains(message, "results")) ||
		strings.Contains(message, "too many results") ||
		(strings.Contains(message, "response size") && limited)
}
