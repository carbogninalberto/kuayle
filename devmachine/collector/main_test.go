package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendRetriesTransientResponsesAndHonorsRetryAfter(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer collector-secret" {
			t.Errorf("unexpected authorization header")
		}
		switch requests {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	var delays []time.Duration
	c := &collector{
		client: server.Client(), endpoint: server.URL, token: "collector-secret",
		wait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	}

	if err := c.send(context.Background(), "collector", "test.event", map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("expected 3 delivery attempts, got %d", requests)
	}
	if len(delays) != 2 {
		t.Fatalf("expected 2 retry delays, got %d", len(delays))
	}
	if delays[0] != 2*time.Second {
		t.Fatalf("expected Retry-After delay, got %s", delays[0])
	}
	if delays[1] < 2*deliveryBaseDelay || delays[1] >= 3*deliveryBaseDelay {
		t.Fatalf("expected exponential delay with bounded jitter, got %s", delays[1])
	}
}

func TestSendDoesNotRetryPermanentResponseOrExposeBody(t *testing.T) {
	const secret = "collector-secret"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(secret))
	}))
	defer server.Close()

	c := &collector{client: server.Client(), endpoint: server.URL, token: secret}
	err := c.send(context.Background(), "collector", "test.event", nil)
	if err == nil {
		t.Fatal("expected permanent delivery failure")
	}
	failure, ok := err.(*deliveryError)
	if !ok || failure.transient || failure.statusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected delivery failure: %#v", err)
	}
	if requests != 1 {
		t.Fatalf("permanent response was retried %d times", requests)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("delivery error exposed response content: %v", err)
	}
}

func TestSendBoundsTransientRetries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var delays []time.Duration
	c := &collector{
		client: server.Client(), endpoint: server.URL, token: "collector-secret",
		wait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	}
	err := c.send(context.Background(), "collector", "test.event", nil)
	failure, ok := err.(*deliveryError)
	if !ok || !failure.transient {
		t.Fatalf("expected transient delivery failure, got %#v", err)
	}
	if requests != deliveryMaxAttempts || len(delays) != deliveryMaxAttempts-1 {
		t.Fatalf("expected %d bounded attempts and %d delays, got %d and %d", deliveryMaxAttempts, deliveryMaxAttempts-1, requests, len(delays))
	}
}

func TestReceiveEventReflectsDeliveryOutcome(t *testing.T) {
	for _, test := range []struct {
		name       string
		upstream   int
		expected   int
		retryAfter string
	}{
		{name: "accepted", upstream: http.StatusAccepted, expected: http.StatusAccepted},
		{name: "permanent rejection", upstream: http.StatusUnauthorized, expected: http.StatusBadGateway},
		{name: "transient exhaustion", upstream: http.StatusServiceUnavailable, expected: http.StatusServiceUnavailable, retryAfter: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.upstream)
			}))
			defer server.Close()
			c := &collector{
				client: server.Client(), endpoint: server.URL, token: "collector-secret",
				wait: func(context.Context, time.Duration) error { return nil },
			}
			request := httptest.NewRequest(http.MethodPost, "/event", strings.NewReader(`{"source":"collector","event_type":"test.event","payload":{}}`))
			recorder := httptest.NewRecorder()

			c.receiveEvent(recorder, request)

			if recorder.Code != test.expected {
				t.Fatalf("expected status %d, got %d", test.expected, recorder.Code)
			}
			if recorder.Header().Get("Retry-After") != test.retryAfter {
				t.Fatalf("expected Retry-After %q, got %q", test.retryAfter, recorder.Header().Get("Retry-After"))
			}
		})
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	delay, ok := parseRetryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now)
	if !ok || delay != 3*time.Second {
		t.Fatalf("expected three-second HTTP-date delay, got %s, %t", delay, ok)
	}
}
