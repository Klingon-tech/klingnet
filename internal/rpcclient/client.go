// Package rpcclient provides a JSON-RPC 2.0 client for klingnet nodes.
package rpcclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a JSON-RPC 2.0 HTTP client.
type Client struct {
	endpoint string
	http     *http.Client
}

// New creates a new RPC client targeting the given endpoint URL.
func New(endpoint string) *Client {
	return NewWithTimeout(endpoint, 10*time.Second)
}

// NewWithTimeout creates a new RPC client with a custom HTTP timeout.
func NewWithTimeout(endpoint string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		endpoint: endpoint,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

// request is a JSON-RPC 2.0 request.
type request struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

// response is a JSON-RPC 2.0 response.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// rpcError is a JSON-RPC 2.0 error.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPCError is returned when the server responds with an error.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Call invokes a JSON-RPC method and unmarshals the result into the provided pointer.
// If result is nil, the response result is discarded.
func (c *Client) Call(method string, params, result interface{}) error {
	req := request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.http.Post(c.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var rpcResp response
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return &RPCError{
			Code:    rpcResp.Error.Code,
			Message: rpcResp.Error.Message,
		}
	}

	if result != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}

	return nil
}
