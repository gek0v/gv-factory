// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// workerLoop drains delivery jobs from the channel and executes deliveries.
func (d *Dispatcher) workerLoop() {
	defer d.wg.Done()

	for job := range d.jobs {
		ctx := d.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		_, _ = d.executeDelivery(ctx, job.ID, job.Subscription, job.Payload)
	}
}

// SendDirect delivers a webhook to a single subscription synchronously with retries and audit logging.
func (d *Dispatcher) SendDirect(ctx context.Context, sub Subscription, payload Payload) (DeliveryRecord, error) {
	if payload.ID == "" {
		payload.ID = generateID("evt")
	}
	if payload.Timestamp == 0 {
		payload.Timestamp = time.Now().UnixMilli()
	}
	deliveryID := generateID("del")
	return d.executeDelivery(ctx, deliveryID, sub, payload)
}

// executeDelivery executes the resilient delivery attempt loop for a single (subscription, payload) pair.
func (d *Dispatcher) executeDelivery(ctx context.Context, deliveryID string, sub Subscription, payload Payload) (DeliveryRecord, error) {
	startTime := time.Now().UTC()
	serialized, err := json.Marshal(payload)
	if err != nil {
		rec := DeliveryRecord{
			ID:             deliveryID,
			SubscriptionID: sub.ID,
			EventID:        payload.ID,
			EventType:      payload.Event,
			TargetURL:      sub.URL,
			Payload:        payload,
			Attempts:       0,
			StatusCode:     0,
			Error:          fmt.Sprintf("json serialization error: %v", err),
			Success:        false,
			Duration:       time.Since(startTime),
			CreatedAt:      startTime,
			CompletedAt:    time.Now().UTC(),
		}
		_ = d.audit.Record(ctx, rec)
		_ = d.dlq.Enqueue(ctx, DeadLetterRecord{
			ID:             generateID("dlq"),
			DeliveryID:     deliveryID,
			SubscriptionID: sub.ID,
			EventID:        payload.ID,
			EventType:      payload.Event,
			TargetURL:      sub.URL,
			Payload:        payload,
			Attempts:       0,
			StatusCode:     0,
			Error:          rec.Error,
			Diagnostic:     "Payload failed JSON marshalling before dispatch",
			CreatedAt:      time.Now().UTC(),
		})
		return rec, err
	}

	sig := BuildSignatureHeader(d.cfg.SignaturePrefix, sub.Secret, serialized)
	timestampStr := strconv.FormatInt(payload.Timestamp, 10)

	policy := d.cfg.RetryPolicy
	maxAttempts := policy.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var (
		lastStatusCode int
		lastRespBody   string
		lastErr        error
		completed      bool
		attempt        int
	)

	for attempt = 1; attempt <= maxAttempts; attempt++ {
		reqTimeout := policy.RequestTimeout
		if reqTimeout <= 0 {
			reqTimeout = 10 * time.Second
		}

		attemptCtx, attemptCancel := context.WithTimeout(ctx, reqTimeout)

		req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodPost, sub.URL, bytes.NewReader(serialized))
		if reqErr != nil {
			attemptCancel()
			lastErr = reqErr
			break
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderSignature, sig)
		req.Header.Set(HeaderTimestamp, timestampStr)
		req.Header.Set(HeaderDelivery, deliveryID)
		req.Header.Set(HeaderEvent, payload.Event)

		for k, v := range sub.Headers {
			req.Header.Set(k, v)
		}

		resp, doErr := d.client.Do(req)
		if doErr != nil {
			attemptCancel()
			lastErr = doErr
			lastStatusCode = 0
		} else {
			lastStatusCode = resp.StatusCode
			var bodyBytes []byte
			if resp.Body != nil {
				bodyBytes, _ = io.ReadAll(io.LimitReader(resp.Body, 16384))
				_ = resp.Body.Close()
			}
			lastRespBody = string(bodyBytes)
			attemptCancel()

			if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
				completed = true
				lastErr = nil
				break
			}

			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, lastRespBody)
		}

		if IsPermanentClientError(lastStatusCode) {
			lastErr = fmt.Errorf("%w (HTTP %d): %s", ErrPermanentClientError, lastStatusCode, lastRespBody)
			break
		}

		if attempt < maxAttempts {
			delay := policy.BackoffDelay(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				lastErr = ctx.Err()
				break
			}
		}
	}

	completedAt := time.Now().UTC()
	duration := completedAt.Sub(startTime)

	errString := ""
	if lastErr != nil {
		errString = lastErr.Error()
	}

	totalAttempts := attempt
	if totalAttempts > maxAttempts {
		totalAttempts = maxAttempts
	}

	rec := DeliveryRecord{
		ID:             deliveryID,
		SubscriptionID: sub.ID,
		EventID:        payload.ID,
		EventType:      payload.Event,
		TargetURL:      sub.URL,
		Payload:        payload,
		Attempts:       totalAttempts,
		StatusCode:     lastStatusCode,
		ResponseBody:   lastRespBody,
		Error:          errString,
		Success:        completed,
		Duration:       duration,
		CreatedAt:      startTime,
		CompletedAt:    completedAt,
	}

	_ = d.audit.Record(ctx, rec)

	if !completed {
		diagnostic := fmt.Sprintf("Delivery failed after %d attempts. Target: %s. Last HTTP status: %d. Error: %v",
			totalAttempts, sub.URL, lastStatusCode, lastErr)
		_ = d.dlq.Enqueue(ctx, DeadLetterRecord{
			ID:             generateID("dlq"),
			DeliveryID:     deliveryID,
			SubscriptionID: sub.ID,
			EventID:        payload.ID,
			EventType:      payload.Event,
			TargetURL:      sub.URL,
			Payload:        payload,
			Attempts:       totalAttempts,
			StatusCode:     lastStatusCode,
			Error:          errString,
			Diagnostic:     diagnostic,
			CreatedAt:      completedAt,
		})
	}

	return rec, lastErr
}
