package okx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const OKXBaseURL = "https://www.okx.com"

type Client struct {
	httpClient *http.Client
	signer     *Signer
	baseURL    string
}

type okxResponse struct {
	Code string          `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func NewClient(apiKey, secretKey, passphrase string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		signer:     NewSigner(apiKey, secretKey, passphrase),
		baseURL:    OKXBaseURL,
	}
}

func (c *Client) DoRequest(ctx context.Context, method, path string, query url.Values, body interface{}) (json.RawMessage, error) {
	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
	}

	requestPath := path
	if query != nil && len(query) > 0 {
		requestPath += "?" + query.Encode()
	}

	timestamp := c.signer.Timestamp()
	signature := c.signer.Sign(timestamp, method, requestPath, string(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+requestPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OK-ACCESS-KEY", c.signer.GetAPIKey())
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.signer.GetPassphrase())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var okxResp okxResponse
	if err := json.Unmarshal(respBody, &okxResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if okxResp.Code != "0" {
		return nil, fmt.Errorf("okx API error: code=%s, msg=%s", okxResp.Code, okxResp.Msg)
	}

	return okxResp.Data, nil
}
