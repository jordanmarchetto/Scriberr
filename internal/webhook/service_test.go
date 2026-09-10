package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scriberr/internal/models"

	"github.com/stretchr/testify/assert"
)

func TestSubscribesTo(t *testing.T) {
	tests := []struct {
		name       string
		eventsJSON string
		event      Event
		want       bool
	}{
		{name: "subscribed", eventsJSON: `["recording.uploaded","transcription.completed"]`, event: EventTranscriptionSuccess, want: true},
		{name: "not subscribed", eventsJSON: `["recording.uploaded"]`, event: EventTranscriptionSuccess, want: false},
		{name: "invalid configuration", eventsJSON: `not-json`, event: EventTranscriptionSuccess, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, subscribesTo(tt.eventsJSON, tt.event))
		})
	}
}

func TestSendEvent(t *testing.T) {
	payload := EventPayload{
		Event:      EventTranscriptionSuccess,
		JobID:      "job-123",
		Status:     models.StatusCompleted,
		AudioPath:  "/path/to/audio.wav",
		OccurredAt: time.Date(2026, time.September, 10, 20, 51, 27, 0, time.UTC),
	}

	t.Run("SignsExactPayload", func(t *testing.T) {
		const secret = "hmactest"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "Scriberr-Webhook/1.0", r.Header.Get("User-Agent"))

			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = mac.Write(body)
			expectedSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
			assert.Equal(t, expectedSignature, r.Header.Get("X-Scriberr-Signature"))

			var received EventPayload
			assert.NoError(t, json.Unmarshal(body, &received))
			assert.Equal(t, payload, received)
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		statusCode, err := NewService().sendEvent(context.Background(), server.URL, secret, payload)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusAccepted, statusCode)
	})

	t.Run("RejectsNonSuccessStatus", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()

		statusCode, err := NewService().sendEvent(context.Background(), server.URL, "", payload)

		assert.Equal(t, http.StatusBadGateway, statusCode)
		assert.EqualError(t, err, "webhook returned status 502")
	})
}

func TestSendWebhook(t *testing.T) {
	// Setup
	service := NewService()
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		// Mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "Scriberr-Webhook/1.0", r.Header.Get("User-Agent"))

			var payload WebhookPayload
			err := json.NewDecoder(r.Body).Decode(&payload)
			assert.NoError(t, err)
			assert.Equal(t, "job-123", payload.JobID)
			assert.Equal(t, models.StatusCompleted, payload.Status)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Test payload
		payload := WebhookPayload{
			JobID:       "job-123",
			Status:      models.StatusCompleted,
			AudioPath:   "/path/to/audio.wav",
			CompletedAt: time.Now(),
		}

		// Execute
		err := service.SendWebhook(ctx, server.URL, payload)

		// Verify
		assert.NoError(t, err)
	})

	t.Run("RetryLogic", func(t *testing.T) {
		attempts := 0
		// Mock server that fails twice then succeeds
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		payload := WebhookPayload{
			JobID:  "job-retry",
			Status: models.StatusFailed,
		}

		// Execute
		err := service.SendWebhook(ctx, server.URL, payload)

		// Verify
		assert.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})

	t.Run("FailureAfterRetries", func(t *testing.T) {
		// Mock server that always fails
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		payload := WebhookPayload{
			JobID: "job-fail",
		}

		// Execute
		err := service.SendWebhook(ctx, server.URL, payload)

		// Verify
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send webhook after 3 attempts")
	})

	t.Run("EmptyURL", func(t *testing.T) {
		err := service.SendWebhook(ctx, "", WebhookPayload{})
		assert.NoError(t, err)
	})
}
