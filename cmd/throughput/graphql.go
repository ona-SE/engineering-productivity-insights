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

// graphqlQuery executes a GraphQL query with retry and rate-limit handling.
func graphqlQuery(token, query string) (*graphqlResponse, error) {
	reqBody := graphqlRequest{Query: query}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("POST", graphqlEndpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		// Retry on server errors (502, 503, etc.)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data[:min(200, len(data))]))
			fmt.Fprintf(os.Stderr, "  Retrying (attempt %d/3): %v\n", attempt, lastErr)
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		var gqlResp graphqlResponse
		if err := json.Unmarshal(data, &gqlResp); err != nil {
			lastErr = fmt.Errorf("unmarshal response: %w (body: %s)", err, string(data[:min(200, len(data))]))
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		// Check for rate limiting
		if len(gqlResp.Errors) > 0 && gqlResp.Errors[0].Type == "RATE_LIMITED" {
			fmt.Fprintf(os.Stderr, "  Rate limited, waiting 60s (attempt %d)...\n", attempt)
			time.Sleep(60 * time.Second)
			lastErr = fmt.Errorf("rate limited")
			continue
		}

		// Retry when data is null/empty (server-side timeout or partial failure)
		if len(gqlResp.Data) == 0 || string(gqlResp.Data) == "null" {
			errMsg := "null data"
			if len(gqlResp.Errors) > 0 {
				errMsg = gqlResp.Errors[0].Message
			}
			lastErr = fmt.Errorf("empty response data: %s", errMsg)
			fmt.Fprintf(os.Stderr, "  Retrying (attempt %d/3): %v\n", attempt, lastErr)
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		return &gqlResp, nil
	}
	return nil, fmt.Errorf("graphql query failed after 3 attempts: %v", lastErr)
}
