// Package retrylimit provides adaptive rate limiting and retry mechanisms
// for building resilient clients. Works with any error types while providing
// special handling for HTTP-related errors.
//
// Example usage:
//
//	lim := retrylimit.NewAdaptiveLimiter(5, 1, 20, 1, 0.5)
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	err := retrylimit.WithRetry(ctx, func() error {
//	    return doSomeWork()
//	}, lim)
package retrylimit

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// =============================================================================
// Limiter
// =============================================================================

// AdaptiveLimiter manages a rate limit that adjusts automatically based
// on the outcome of requests. It increases on success and decreases on
// errors. Thread-safe and works with any error types.
type AdaptiveLimiter struct {
	mu        sync.RWMutex
	limiter   *rate.Limiter
	minLimit  rate.Limit
	maxLimit  rate.Limit
	stepUp    rate.Limit
	stepDown  float64
	lastError time.Time
}

// NewAdaptiveLimiter creates an AdaptiveLimiter with the given configuration.
//
// Parameters:
//   - initial: starting requests per second
//   - min: minimum allowed rate
//   - max: maximum allowed rate
//   - stepUp: increment on success
//   - stepDown: multiplier applied on failure (e.g., 0.5 to halve)
func NewAdaptiveLimiter(initial, min, max rate.Limit, stepUp rate.Limit, stepDown float64) *AdaptiveLimiter {
	if initial < 1 {
		initial = 1
	}
	if min < 1 {
		min = 1
	}
	burst := maxInt(1, int(initial))
	return &AdaptiveLimiter{
		limiter:  rate.NewLimiter(initial, burst),
		minLimit: min,
		maxLimit: max,
		stepUp:   stepUp,
		stepDown: stepDown,
	}
}

// Wait blocks until a token is available or the context is canceled.
func (a *AdaptiveLimiter) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return a.limiter.Wait(ctx)
}

// Success increases the rate after a successful request.
func (a *AdaptiveLimiter) Success() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.lastError) > 10*time.Second {
		a.adjustLimit(a.limiter.Limit() + a.stepUp)
	}
}

// RateLimited reduces the rate after a failure or server response indicating overload.
func (a *AdaptiveLimiter) RateLimited() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastError = time.Now()
	newLimit := rate.Limit(float64(a.limiter.Limit()) * a.stepDown)
	a.adjustLimit(newLimit)
}

// CurrentLimit returns the current requests per second.
func (a *AdaptiveLimiter) CurrentLimit() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return float64(a.limiter.Limit())
}

// CurrentBurst returns the current burst size.
func (a *AdaptiveLimiter) CurrentBurst() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.limiter.Burst()
}

// MaxLimit returns the configured maximum rate.
func (a *AdaptiveLimiter) MaxLimit() rate.Limit { return a.maxLimit }

// MinLimit returns the configured minimum rate.
func (a *AdaptiveLimiter) MinLimit() rate.Limit { return a.minLimit }

// adjustLimit sets the limiter to a new rate, respecting min/max boundaries.
func (a *AdaptiveLimiter) adjustLimit(newLimit rate.Limit) {
	oldLimit := a.limiter.Limit()

	if newLimit > a.maxLimit {
		newLimit = a.maxLimit
	} else if newLimit < a.minLimit {
		newLimit = a.minLimit
	}

	if newLimit != oldLimit {
		a.limiter.SetLimit(newLimit)
		a.limiter.SetBurst(maxInt(1, int(newLimit)))
	}
}

// =============================================================================
// Errors
// =============================================================================

// HTTPError interface for errors that carry HTTP status codes.
// Optional interface - errors don't need to implement this for basic retry.
type HTTPError interface {
	error
	StatusCode() int
}

// FatalError wraps errors that should stop retries immediately.
type FatalError struct {
	Err error
}

func (f *FatalError) Error() string { return f.Err.Error() }
func (f *FatalError) Unwrap() error { return f.Err }

// ErrorClassifier allows custom error classification for retry logic.
// Return true if the error should trigger rate limiting.
type ErrorClassifier func(error) bool

// DefaultClassifier provides HTTP-aware error classification.
// Returns true for 429 (rate limit) and 5xx (server errors).
func DefaultClassifier(err error) bool {
	return isRateLimitError(err) || isServerError(err)
}

// =============================================================================
// Retry
// =============================================================================

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts     int                          // Maximum number of attempts (0 = unlimited, capped at 100)
	InitialDelay    time.Duration                // Initial delay between retries
	MaxDelay        time.Duration                // Maximum delay between retries
	RateLimitDelay  time.Duration                // Fixed delay for rate limit errors
	Multiplier      float64                      // Delay multiplier for exponential backoff
	Jitter          bool                         // Add random jitter to prevent thundering herd
	ErrorClassifier ErrorClassifier              // Custom error classifier (nil = use DefaultClassifier)
	OnRetry         func(attempt int, err error) // Optional callback on each retry
}

// DefaultRetryConfig returns a sensible default configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:     100,
		InitialDelay:    500 * time.Millisecond,
		MaxDelay:        10 * time.Second,
		RateLimitDelay:  100 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          true,
		ErrorClassifier: DefaultClassifier,
	}
}

// WithRetry executes a function with exponential backoff and optional adaptive rate limiting.
// Stops retrying if:
//   - fn returns nil (success)
//   - fn returns FatalError
//   - context is cancelled or expires
//   - maximum attempts is reached (if configured)
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	lim := retrylimit.NewAdaptiveLimiter(5, 1, 10, 1, 0.5)
//	err := retrylimit.WithRetry(ctx, func() error { return MyRequest() }, lim)
func WithRetry(ctx context.Context, fn func() error, lim *AdaptiveLimiter) error {
	return WithRetryConfig(ctx, fn, lim, DefaultRetryConfig())
}

// WithRetryMax executes fn with exponential backoff up to maxAttempts times.
// Stops immediately if fn returns FatalError or context is cancelled.
func WithRetryMax(ctx context.Context, fn func() error, lim *AdaptiveLimiter, maxAttempts int) error {
	cfg := DefaultRetryConfig()
	cfg.MaxAttempts = maxAttempts
	return WithRetryConfig(ctx, fn, lim, cfg)
}

// WithRetryConfig executes fn with custom retry configuration.
func WithRetryConfig(ctx context.Context, fn func() error, lim *AdaptiveLimiter, cfg RetryConfig) error {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 100 // safety limit for "unlimited"
	}
	if cfg.ErrorClassifier == nil {
		cfg.ErrorClassifier = DefaultClassifier
	}

	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Wait for rate limiter permission before making request
		if lim != nil {
			if err := lim.Wait(ctx); err != nil {
				return err
			}
		}

		err := fn()
		if err == nil {
			if lim != nil {
				lim.Success()
				if attempt > 1 {
					log.Printf("[Retry] Success after %d attempts. Limiter=%.2f rps",
						attempt, lim.CurrentLimit())
				}
			}
			return nil
		}

		// Check for fatal errors
		if isFatalError(err) {
			return err
		}

		// Call user callback if provided
		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt, err)
		}

		// Handle rate limit errors with special treatment
		if isRateLimitError(err) {
			if lim != nil {
				lim.RateLimited()
				log.Printf("[Retry] Rate limit (attempt %d). New limit: %.2f rps",
					attempt, lim.CurrentLimit())
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cfg.RateLimitDelay):
			}
			continue
		}

		// Handle server errors or errors matching custom classifier
		shouldRateLimit := cfg.ErrorClassifier(err)
		if shouldRateLimit && lim != nil {
			lim.RateLimited()
		}

		if isServerError(err) {
			log.Printf("[Retry] Server error (attempt %d): %v. Sleeping %v",
				attempt, err, delay)
		} else {
			log.Printf("[Retry] Request failed (attempt %d): %v. Sleeping %v",
				attempt, err, delay)
		}

		// Calculate next delay with optional jitter
		nextDelay := delay
		if cfg.Jitter {
			nextDelay = addJitter(delay)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(nextDelay):
		}

		// Increase delay for next attempt
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	return fmt.Errorf("max attempts (%d) exceeded", cfg.MaxAttempts)
}

// =============================================================================
// Helper functions
// =============================================================================

// addJitter adds random jitter (0-25% of delay) to prevent thundering herd problem.
func addJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	jitter := time.Duration(rand.Int63n(int64(delay / 4)))
	return delay + jitter
}

// isFatalError returns true if err is of type FatalError.
func isFatalError(err error) bool {
	_, ok := err.(*FatalError)
	return ok
}

// isRateLimitError returns true if err implements HTTPError and code == 429.
func isRateLimitError(err error) bool {
	if httpErr, ok := err.(HTTPError); ok {
		return httpErr.StatusCode() == http.StatusTooManyRequests
	}
	return false
}

// isServerError returns true if err implements HTTPError and code is 5xx.
func isServerError(err error) bool {
	if httpErr, ok := err.(HTTPError); ok {
		code := httpErr.StatusCode()
		return code >= 500 && code < 600
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
