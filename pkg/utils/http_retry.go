package utils

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const maxRetries = 5

var retryDelayUnit = time.Second

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode >= 500
}

// retryDelay returns the delay before the next retry attempt.
// For 429 responses, it respects the Retry-After header if present,
// otherwise uses exponential backoff (2s, 4s, 8s, 16s).
// For 5xx errors, uses linear backoff (1s, 2s, 3s, 4s).
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs > 0 && secs <= 120 {
				return time.Duration(secs) * time.Second
			}
		}
		// Exponential backoff for 429 without Retry-After
		return retryDelayUnit * time.Duration(1<<uint(attempt))
	}
	// Linear backoff for 5xx
	return retryDelayUnit * time.Duration(attempt+1)
}

func DoRequestWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for i := range maxRetries {
		if i > 0 && resp != nil {
			resp.Body.Close()
		}

		resp, err = client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				break
			}
			if !shouldRetry(resp.StatusCode) {
				break
			}
		}

		if i < maxRetries-1 {
			delay := retryDelay(resp, i)
			if err = sleepWithCtx(req.Context(), delay); err != nil {
				if resp != nil {
					resp.Body.Close()
				}
				return nil, fmt.Errorf("failed to sleep: %w", err)
			}
		}
	}
	return resp, err
}

func sleepWithCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
