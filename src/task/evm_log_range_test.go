package task

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/model/request"
	"github.com/GMWalletApp/epusdt/model/service"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

func logQueryRange(params json.RawMessage) (int64, int64, error) {
	var filters []struct {
		From string `json:"fromBlock"`
		To   string `json:"toBlock"`
	}
	if err := json.Unmarshal(params, &filters); err != nil || len(filters) != 1 {
		return 0, 0, fmt.Errorf("invalid log query: %s", params)
	}
	from, err := strconv.ParseInt(strings.TrimPrefix(filters[0].From, "0x"), 16, 64)
	if err != nil {
		return 0, 0, err
	}
	to, err := strconv.ParseInt(strings.TrimPrefix(filters[0].To, "0x"), 16, 64)
	return from, to, err
}

func TestEvmRangeLimitedBackfillSettlesAndResumes(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	seedEvmHealthProbeState(t)
	if err := dao.Mdb.Model(&mdb.Chain{}).Where("network = ?", mdb.NetworkBsc).Updates(map[string]any{"min_confirmations": 1, "scan_interval_sec": 1}).Error; err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
	var trades []string
	var events []types.Log
	for i, block := range []uint64{101, 112} {
		order, err := service.CreateTransaction(&request.CreateTransactionRequest{OrderId: fmt.Sprintf("range_%d", i), Amount: float64(i + 1), Currency: "CNY", Token: "USDT", Network: mdb.NetworkBsc, NotifyUrl: "https://93.184.216.34/notify"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		trades = append(trades, order.TradeId)
		value := new(big.Int).Mul(big.NewInt(int64(i+1)), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
		events = append(events, types.Log{Address: contract, Topics: []common.Hash{transferEventHash, {}, common.BytesToHash(address.Bytes())}, Data: value.FillBytes(make([]byte, 32)), BlockNumber: block, TxHash: common.BytesToHash([]byte(order.TradeId))})
	}
	timestamp := uint64(time.Now().Unix() + 5)
	var head atomic.Int64
	head.Store(112)
	var mu sync.Mutex
	var covered [][2]int64
	server := newEvmHealthHTTPServer(t, func(method string, params json.RawMessage) (interface{}, *rpcHealthTestError) {
		switch method {
		case "eth_getBlockByNumber":
			var args []json.RawMessage
			if err := json.Unmarshal(params, &args); err != nil || len(args) == 0 {
				return nil, &rpcHealthTestError{Code: -32602, Message: "bad header request"}
			}
			var raw string
			if err := json.Unmarshal(args[0], &raw); err != nil {
				return nil, &rpcHealthTestError{Code: -32602, Message: "bad block"}
			}
			n := head.Load()
			if raw != "latest" {
				var err error
				n, err = strconv.ParseInt(strings.TrimPrefix(raw, "0x"), 16, 64)
				if err != nil {
					return nil, &rpcHealthTestError{Code: -32602, Message: "bad block number"}
				}
			}
			return &types.Header{Number: big.NewInt(n), Difficulty: big.NewInt(0), Time: timestamp, Extra: []byte{}, UncleHash: types.EmptyUncleHash, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash}, nil
		case "eth_getLogs":
			from, to, err := logQueryRange(params)
			if err != nil {
				return nil, &rpcHealthTestError{Code: -32602, Message: err.Error()}
			}
			if to-from+1 > 3 {
				return nil, &rpcHealthTestError{Code: -32602, Message: "eth_getLogs is limited to 3 blocks range"}
			}
			mu.Lock()
			covered = append(covered, [2]int64{from, to})
			mu.Unlock()
			logs := []types.Log{}
			for _, event := range events {
				if int64(event.BlockNumber) >= from && int64(event.BlockNumber) <= to {
					logs = append(logs, event)
				}
			}
			return logs, nil
		default:
			return nil, &rpcHealthTestError{Code: -32601, Message: "unknown method"}
		}
	})
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	runUntil := func(want int64) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- runEvmBackfillLoop(ctx, client, mdb.NetworkBsc, "[TEST]", mdb.RpcNode{}, ethereum.FilterQuery{Addresses: []common.Address{contract}}, func(a common.Address) bool { return a == address })
		}()
		defer func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("backfill: %v", err)
			}
		}()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case err := <-done:
				done <- err
				t.Fatalf("scanner stopped before cursor %d: %v", want, err)
			case <-deadline.C:
				t.Fatalf("cursor did not reach %d", want)
			case <-ticker.C:
				cursor, err := data.GetEvmScanCursor(mdb.NetworkBsc)
				if err != nil {
					t.Fatal(err)
				}
				if cursor.LastBlock == want {
					return
				}
			}
		}
	}
	runUntil(112)
	for _, trade := range trades {
		order, err := data.GetOrderInfoByTradeId(trade)
		if err != nil || order.Status != mdb.StatusPaySuccess {
			t.Fatalf("payment at range boundary was lost: %+v err=%v", order, err)
		}
	}
	head.Store(115)
	runUntil(115)
	mu.Lock()
	defer mu.Unlock()
	next := int64(101)
	for _, span := range covered {
		if span[0] != next || span[1] < span[0] || span[1]-span[0]+1 > 3 {
			t.Fatalf("gap or overlap in completed ranges: %v", covered)
		}
		next = span[1] + 1
	}
	if next != 116 {
		t.Fatalf("incomplete coverage: %v", covered)
	}
}

func TestEvmHealthProbeAcceptsRangeLimitedNode(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	seedEvmHealthProbeState(t)
	server := newEvmHealthHTTPServer(t, func(method string, params json.RawMessage) (interface{}, *rpcHealthTestError) {
		if method == "eth_blockNumber" {
			return "0x100", nil
		}
		from, to, err := logQueryRange(params)
		if err != nil {
			return nil, &rpcHealthTestError{Code: -32602, Message: err.Error()}
		}
		if to-from+1 > 10 {
			return nil, &rpcHealthTestError{Code: -32000, Message: "block range exceeds maximum of 10"}
		}
		return []types.Log{}, nil
	})
	defer server.Close()
	if status, _ := ProbeRpcNode(mdb.RpcNode{Network: mdb.NetworkBsc, Type: mdb.RpcNodeTypeHttp, Url: server.URL}); status != mdb.RpcNodeStatusOk {
		t.Fatalf("usable range-limited node marked %s", status)
	}
	cursor, err := data.GetEvmScanCursor(mdb.NetworkBsc)
	if err != nil || cursor.LastBlock != 100 {
		t.Fatalf("health check changed scan cursor: %+v err=%v", cursor, err)
	}
}

func TestEvmLogFailuresDoNotRetryQuota(t *testing.T) {
	for _, failure := range []struct {
		name    string
		code    int
		message string
	}{
		{"quota", -32005, "rate limit exceeded"},
		{"ambiguous", -32005, "limit exceeded"},
		{"authentication", -32000, "unauthorized"},
		{"disabled", -32601, "method not found"},
		{"invalid_filter", -32602, "invalid address"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			var calls atomic.Int64
			server := newEvmHealthHTTPServer(t, func(string, json.RawMessage) (interface{}, *rpcHealthTestError) {
				calls.Add(1)
				return nil, &rpcHealthTestError{Code: failure.code, Message: failure.message}
			})
			defer server.Close()
			client, err := ethclient.Dial(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			query := ethereum.FilterQuery{FromBlock: big.NewInt(101), ToBlock: big.NewInt(150)}
			if _, _, err := filterEvmLogsWithRangeLimit(context.Background(), client, query); err == nil {
				t.Fatal("RPC failure was swallowed")
			}
			if calls.Load() != 1 {
				t.Fatalf("non-range failure retried %d times", calls.Load())
			}
		})
	}
}

func TestEvmBackfillFailurePreservesCursor(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()
	seedEvmHealthProbeState(t)
	server := newEvmHealthHTTPServer(t, func(method string, _ json.RawMessage) (interface{}, *rpcHealthTestError) {
		if method == "eth_getBlockByNumber" {
			return &types.Header{Number: big.NewInt(256), Difficulty: big.NewInt(0), Time: uint64(time.Now().Unix()), Extra: []byte{}, UncleHash: types.EmptyUncleHash, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash}, nil
		}
		return nil, &rpcHealthTestError{Code: -32005, Message: "rate limit exceeded"}
	})
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	err = runEvmBackfillLoop(context.Background(), client, mdb.NetworkBsc, "[TEST]", mdb.RpcNode{}, ethereum.FilterQuery{}, func(common.Address) bool { return false })
	if err == nil {
		t.Fatal("failed RPC did not stop this scan attempt")
	}
	cursor, err := data.GetEvmScanCursor(mdb.NetworkBsc)
	if err != nil || cursor.LastBlock != 100 {
		t.Fatalf("failed query advanced cursor: %+v err=%v", cursor, err)
	}
}

func TestEvmLogRangeRetryStopsAtOneBlockAndCancellation(t *testing.T) {
	var calls atomic.Int64
	server := newEvmHealthHTTPServer(t, func(string, json.RawMessage) (interface{}, *rpcHealthTestError) {
		calls.Add(1)
		return nil, &rpcHealthTestError{Code: -32005, Message: "query returned more than 10000 results"}
	})
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	query := ethereum.FilterQuery{FromBlock: big.NewInt(101), ToBlock: big.NewInt(104)}
	if _, end, err := filterEvmLogsWithRangeLimit(context.Background(), client, query); err == nil || end != 101 {
		t.Fatalf("single block failure: end=%d err=%v", end, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("unbounded retries: %d", calls.Load())
	}
	if query.ToBlock.Int64() != 104 {
		t.Fatal("mutated caller's query")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := filterEvmLogsWithRangeLimit(ctx, client, query); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatal("cancelled request made another RPC call")
	}
}
