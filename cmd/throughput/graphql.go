package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const graphqlEndpoint = "https://api.github.com/graphql"

var httpClient = &http.Client{
	Timeout: 60 * time.Second,
}

type graphqlRequest struct {
	Query string `json:"query"`
}

type graphqlResponse struct {
	Data   json.RawMessage  `json:"data"`
	Errors []graphqlError   `json:"errors"`
}

type graphqlError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

const maxRetries = 6

// retryDelay returns an exponential backoff duration: 2s, 4s, 8s, 16s, 32s, 64s.
func retryDelay(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 64*time.Second {
		d = 64 * time.Second
	}
	return d
}

// graphqlQuery executes a GraphQL query with retry and rate-limit handling.
func graphqlQuery(token, query string) (*graphqlResponse, error) {
	reqBody := graphqlRequest{Query: query}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("POST", graphqlEndpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			delay := retryDelay(attempt)
			fmt.Fprintf(os.Stderr, "  ⚠ Request failed — retrying in %s (attempt %d/%d)\n", delay.Round(time.Second), attempt, maxRetries)
			time.Sleep(delay)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			delay := retryDelay(attempt)
			fmt.Fprintf(os.Stderr, "  ⚠ Read failed — retrying in %s (attempt %d/%d)\n", delay.Round(time.Second), attempt, maxRetries)
			time.Sleep(delay)
			continue
		}

		// Retry on server errors (502, 503, etc.)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
			delay := retryDelay(attempt)
			fmt.Fprintf(os.Stderr, "  ⚠ %v — retrying in %s (attempt %d/%d)\n", lastErr, delay.Round(time.Second), attempt, maxRetries)
			time.Sleep(delay)
			continue
		}

		// Retry on 429 Too Many Requests
		if resp.StatusCode == 429 {
			lastErr = fmt.Errorf("HTTP 429 (Too Many Requests)")
			delay := retryDelay(attempt)
			fmt.Fprintf(os.Stderr, "  ⚠ Rate limited (HTTP 429) — retrying in %s (attempt %d/%d)\n", delay.Round(time.Second), attempt, maxRetries)
			time.Sleep(delay)
			continue
		}

		var gqlResp graphqlResponse
		if err := json.Unmarshal(data, &gqlResp); err != nil {
			lastErr = fmt.Errorf("invalid JSON response (HTTP %d)", resp.StatusCode)
			delay := retryDelay(attempt)
			fmt.Fprintf(os.Stderr, "  ⚠ %v — retrying in %s (attempt %d/%d)\n", lastErr, delay.Round(time.Second), attempt, maxRetries)
			time.Sleep(delay)
			continue
		}

		// Check for rate limiting
		if len(gqlResp.Errors) > 0 && gqlResp.Errors[0].Type == "RATE_LIMITED" {
			lastErr = fmt.Errorf("rate limited")
			fmt.Fprintf(os.Stderr, "  ⚠ GraphQL rate limited — waiting 60s (attempt %d/%d)\n", attempt, maxRetries)
			time.Sleep(60 * time.Second)
			continue
		}

		// Retry when data is null/empty (server-side timeout or partial failure)
		if len(gqlResp.Data) == 0 || string(gqlResp.Data) == "null" {
			errMsg := "empty response"
			if len(gqlResp.Errors) > 0 {
				errMsg = gqlResp.Errors[0].Message
			}
			lastErr = fmt.Errorf("%s", errMsg)
			delay := retryDelay(attempt)
			fmt.Fprintf(os.Stderr, "  ⚠ %v — retrying in %s (attempt %d/%d)\n", lastErr, delay.Round(time.Second), attempt, maxRetries)
			time.Sleep(delay)
			continue
		}

		return &gqlResp, nil
	}
	return nil, fmt.Errorf("graphql query failed after %d attempts: %v", maxRetries, lastErr)
}
