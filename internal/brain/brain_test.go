// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewService_InvalidURL(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		DatabaseURL: "postgres://myuser:super_secret_pw@invalid host with spaces:::5432/mydb",
	}

	svc, err := NewService(ctx, cfg)
	if err == nil {
		if svc != nil {
			svc.Close()
		}
		t.Fatal("expected error for malformed DatabaseURL, got nil")
	}

	if strings.Contains(err.Error(), "super_secret_pw") {
		t.Fatalf("error message leaked password: %s", err.Error())
	}
}

func TestNewService_GracefulDegradationWhenUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Port 59999 is typically unused on localhost
	cfg := Config{
		DatabaseURL: "postgres://ov_user:secret_ov_pass@127.0.0.1:59999/ov_factory?sslmode=disable&connect_timeout=1",
		ConnTimeout: 100 * time.Millisecond,
	}

	svc, err := NewService(ctx, cfg)
	if err != nil {
		t.Fatalf("expected NewService to degrade gracefully without error, got: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	defer svc.Close()

	if svc.Pool() == nil {
		t.Fatal("expected non-nil pool instance on service")
	}

	// Ping should report unreachable database
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer pingCancel()

	pingErr := svc.Ping(pingCtx)
	if pingErr == nil {
		t.Fatal("expected ping to fail on unreachable host, got nil")
	}

	var connErr *ConnectionError
	if !errors.As(pingErr, &connErr) {
		t.Fatalf("expected *ConnectionError, got %T: %v", pingErr, pingErr)
	}

	if strings.Contains(pingErr.Error(), "secret_ov_pass") {
		t.Fatalf("ping error leaked password: %s", pingErr.Error())
	}
}

func TestPing_TimeoutOrCanceled(t *testing.T) {
	cfg := Config{
		DatabaseURL: "postgres://ov_user:secret_ov_pass@127.0.0.1:59999/ov_factory?sslmode=disable&connect_timeout=1",
		ConnTimeout: 50 * time.Millisecond,
	}

	svc, err := NewService(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}
	defer svc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	err = svc.Ping(ctx)
	if err == nil {
		t.Fatal("expected error on pre-canceled context, got nil")
	}
	if strings.Contains(err.Error(), "secret_ov_pass") {
		t.Fatalf("error leaked secret: %s", err.Error())
	}
}

func TestService_ThreadSafety(t *testing.T) {
	cfg := Config{
		DatabaseURL: "postgres://ov_user:pass@127.0.0.1:59999/ov_factory?sslmode=disable&connect_timeout=1",
		ConnTimeout: 50 * time.Millisecond,
	}

	svc, err := NewService(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer svc.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_ = svc.Ping(ctx)
			_ = svc.Pool()
		}()
	}
	wg.Wait()
}

func TestNewService_DefaultsAndCustomConfig(t *testing.T) {
	t.Run("empty DatabaseURL falls back to environment/default config", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		svc, err := NewService(context.Background(), Config{})
		if err != nil {
			t.Fatalf("expected NewService with empty config to succeed, got: %v", err)
		}
		defer svc.Close()

		if svc.Pool() == nil {
			t.Fatal("expected non-nil pool")
		}
	})

	t.Run("custom pool settings applied without error", func(t *testing.T) {
		cfg := Config{
			DatabaseURL: "postgres://ov_user:ov_password@127.0.0.1:59999/ov_factory?sslmode=disable",
			MaxConns:    20,
			MinConns:    4,
			MaxConnIdle: 10 * time.Minute,
			MaxConnLife: 2 * time.Hour,
			ConnTimeout: 200 * time.Millisecond,
		}

		svc, err := NewService(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected custom pool config to succeed, got: %v", err)
		}
		defer svc.Close()

		if svc.Pool() == nil {
			t.Fatal("expected non-nil pool")
		}
	})
}

func TestPing_NilPool(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/db"},
	}
	err := svc.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error on nil pool, got nil")
	}
	var connErr *ConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected *ConnectionError, got %T", err)
	}
	if !strings.Contains(err.Error(), "connection pool is not initialized") {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestConnectionError_Methods(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *ConnectionError
		if nilErr.Error() != "<nil>" {
			t.Fatalf("expected <nil>, got %s", nilErr.Error())
		}
		if nilErr.Unwrap() != nil {
			t.Fatal("expected nil unwrap")
		}
	})

	t.Run("unwrap wrapped error", func(t *testing.T) {
		inner := errors.New("underlying network failure")
		connErr := &ConnectionError{
			Op:  "test_op",
			URL: "postgres://host:5432/db",
			Err: inner,
		}
		if !errors.Is(connErr, inner) {
			t.Fatalf("expected errors.Is to match wrapped error")
		}
		if connErr.Unwrap() != inner {
			t.Fatalf("expected Unwrap() to return inner error")
		}
	})
}
