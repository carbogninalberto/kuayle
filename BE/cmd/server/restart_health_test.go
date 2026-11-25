package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// An apply and restart is confirmed only when the process answers its health
// check and reaches its database.
func TestHTTPHealthProbe(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(int(status.Load())) }))
	defer server.Close()
	ok := func(context.Context) error { return nil }

	require.Error(t, httpHealthProbe(server.URL, ok)(context.Background()), "an unhealthy answer is not healthy")
	status.Store(http.StatusOK)
	require.NoError(t, httpHealthProbe(server.URL, ok)(context.Background()))
	require.Error(t, httpHealthProbe(server.URL, func(context.Context) error { return errors.New("down") })(context.Background()),
		"a database that does not answer is not healthy")
	server.Close()
	require.Error(t, httpHealthProbe(server.URL, ok)(context.Background()), "a process that does not listen is not healthy")
}
