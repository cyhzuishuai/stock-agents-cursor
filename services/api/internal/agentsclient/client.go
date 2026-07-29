package agentsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	HTTP                                                     *http.Client
	RuntimeURL                                               string
	DataURL, ResearchURL, DecisionURL, PortfolioURL, RiskURL string
	MaxRetries                                               int
}

func (c *Client) Call(ctx context.Context, baseURL string, body any, timeout time.Duration) (json.RawMessage, error) {
	maxRetries := c.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	url := strings.TrimSuffix(baseURL, "/") + "/v1/run"
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			cancel()
			if shouldRetryError(err) && attempt < maxRetries {
				lastErr = err
				continue
			}
			return nil, err
		}

		// Keep reqCtx alive until the body is fully read; cancel() right after Do
		// aborts large agent envelopes (result+trace) with "context canceled".
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode >= 500 && attempt < maxRetries {
			lastErr = fmt.Errorf("agent returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("agent returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		return json.RawMessage(raw), nil
	}

	return nil, lastErr
}

func (c *Client) Resume(ctx context.Context, baseURL string, body any, timeout time.Duration) (json.RawMessage, error) {
	// Unlike Call, Resume does not retry on 5xx or transport timeouts. /v1/resume is
	// not idempotent: a timeout may mean the graph already continued server-side, and
	// a retry can return 409 and leave the workflow chain stuck.

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	url := strings.TrimSuffix(baseURL, "/") + "/v1/resume"
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}

	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	cancel()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agent returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	return json.RawMessage(raw), nil
}

func shouldRetryError(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
