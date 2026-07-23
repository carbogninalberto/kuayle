package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestCDPProxyRequiresTokenAndOnlyForwardsTargetListing(t *testing.T) {
	var upstreamRequests atomic.Int32
	var upstreamHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		upstreamRequests.Add(1)
		if request.Host != upstreamHost {
			t.Errorf("unexpected upstream host %q", request.Host)
		}
		if request.Header.Get("Authorization") != "" {
			t.Error("CDP credential was forwarded to Chrome")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[{"id":"page-1","type":"page"}]`))
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstreamHost = upstreamURL.Host
	handler := newCDPProxyHandler("dedicated-cdp-token", upstreamURL)

	for _, test := range []struct {
		name          string
		method        string
		path          string
		authorization string
		status        int
		forwarded     bool
	}{
		{name: "missing token", method: http.MethodGet, path: "/json", status: http.StatusUnauthorized},
		{name: "wrong token", method: http.MethodGet, path: "/json", authorization: "Bearer wrong", status: http.StatusUnauthorized},
		{name: "control endpoint", method: http.MethodGet, path: "/json/version", authorization: "Bearer dedicated-cdp-token", status: http.StatusNotFound},
		{name: "mutation", method: http.MethodPost, path: "/json", authorization: "Bearer dedicated-cdp-token", status: http.StatusMethodNotAllowed},
		{name: "target listing", method: http.MethodGet, path: "/json", authorization: "Bearer dedicated-cdp-token", status: http.StatusOK, forwarded: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := upstreamRequests.Load()
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("expected status %d, got %d", test.status, response.Code)
			}
			if (upstreamRequests.Load() > before) != test.forwarded {
				t.Fatalf("forwarded=%t, expected %t", upstreamRequests.Load() > before, test.forwarded)
			}
		})
	}
}
