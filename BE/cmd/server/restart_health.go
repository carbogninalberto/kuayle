package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// apiHealthProbe checks that this process serves requests and reaches its
// database; an apply and restart is confirmed only after it succeeds.
func apiHealthProbe(port int, db *sqlx.DB) func(context.Context) error {
	return httpHealthProbe(fmt.Sprintf("http://127.0.0.1:%d/health", port), db.PingContext)
}

func httpHealthProbe(url string, ping func(context.Context) error) func(context.Context) error {
	client := &http.Client{Timeout: 3 * time.Second}
	return func(ctx context.Context) error {
		if err := ping(ctx); err != nil {
			return fmt.Errorf("database: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("health check answered %d", res.StatusCode)
		}
		return nil
	}
}
