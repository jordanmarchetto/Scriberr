package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"scriberr/internal/models"
	"scriberr/pkg/logger"
	"gorm.io/gorm"
)

type Event string

const (
	EventRecordingUploaded Event = "recording.uploaded"
	EventTranscriptionSuccess Event = "transcription.completed"
	EventTranscriptionFailed Event = "transcription.failed"
	EventSummarySuccess Event = "summary.completed"
	EventSummaryFailed Event = "summary.failed"
)

var AllEvents = []Event{EventRecordingUploaded, EventTranscriptionSuccess, EventTranscriptionFailed, EventSummarySuccess, EventSummaryFailed}

type EventPayload struct {
	Event Event `json:"event"`
	JobID string `json:"job_id"`
	Title *string `json:"title,omitempty"`
	Status models.JobStatus `json:"status"`
	AudioPath string `json:"audio_path"`
	Transcript *string `json:"transcript,omitempty"`
	Summary *string `json:"summary,omitempty"`
	Error string `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// WebhookPayload represents the data sent to the callback URL
type WebhookPayload struct {
	JobID        string                 `json:"job_id"`
	Status       models.JobStatus       `json:"status"`
	AudioPath    string                 `json:"audio_path"`
	Transcript   *string                `json:"transcript,omitempty"`
	Summary      *string                `json:"summary,omitempty"`
	ErrorMessage *string                `json:"error_message,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CompletedAt  time.Time              `json:"completed_at"`
}

// Service handles webhook operations
type Service struct {
	client *http.Client
	db *gorm.DB
}

func (s *Service) SetDatabase(db *gorm.DB) { s.db = db }

// Dispatch sends an event to every enabled webhook subscribed to it.
func (s *Service) Dispatch(ctx context.Context, event Event, job *models.TranscriptionJob, metadata map[string]interface{}, errorMessage string) {
	if s.db == nil || job == nil { return }
	var hooks []models.Webhook
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&hooks).Error; err != nil { logger.Error("Failed to load webhooks", "error", err); return }
	for _, hook := range hooks {
		var events []string
		if json.Unmarshal([]byte(hook.Events), &events) != nil { continue }
		matched := false
		for _, configured := range events { if configured == string(event) { matched = true; break } }
		if !matched { continue }
		payload := EventPayload{Event: event, JobID: job.ID, Title: job.Title, Status: job.Status, AudioPath: job.AudioPath, Transcript: job.Transcript, Summary: job.Summary, Error: errorMessage, Metadata: metadata, OccurredAt: time.Now().UTC()}
		secret := ""; if hook.Secret != nil { secret = *hook.Secret }
		go func(h models.Webhook, p EventPayload, secret string) {
			requestCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second); defer cancel()
			if err := s.sendEvent(requestCtx, h.URL, secret, p); err != nil { logger.Error("Failed to send configured webhook", "webhook_id", h.ID, "event", p.Event, "error", err) }
		}(hook, payload, secret)
	}
}

func (s *Service) sendEvent(ctx context.Context, url, secret string, payload EventPayload) error {
	data, err := json.Marshal(payload); if err != nil { return err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data)); if err != nil { return err }
	req.Header.Set("Content-Type", "application/json"); req.Header.Set("User-Agent", "Scriberr-Webhook/1.0")
	if secret != "" { mac := hmac.New(sha256.New, []byte(secret)); _, _ = mac.Write(data); req.Header.Set("X-Scriberr-Signature", "sha256="+fmt.Sprintf("%x", mac.Sum(nil))) }
	resp, err := s.client.Do(req); if err != nil { return err }; defer resp.Body.Close(); _, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("webhook returned status %d", resp.StatusCode) }; return nil
}

func (s *Service) List(ctx context.Context) ([]models.Webhook, error) {
	var hooks []models.Webhook
	if s.db == nil { return hooks, fmt.Errorf("webhook database is not configured") }
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&hooks).Error
	return hooks, err
}

func (s *Service) Create(ctx context.Context, hook *models.Webhook) error {
	if s.db == nil { return fmt.Errorf("webhook database is not configured") }
	return s.db.WithContext(ctx).Create(hook).Error
}

func (s *Service) Update(ctx context.Context, hook *models.Webhook) error {
	if s.db == nil { return fmt.Errorf("webhook database is not configured") }
	return s.db.WithContext(ctx).Save(hook).Error
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if s.db == nil { return fmt.Errorf("webhook database is not configured") }
	return s.db.WithContext(ctx).Delete(&models.Webhook{}, "id = ?", id).Error
}

func (s *Service) Get(ctx context.Context, id string) (*models.Webhook, error) {
	var hook models.Webhook
	if s.db == nil { return nil, fmt.Errorf("webhook database is not configured") }
	if err := s.db.WithContext(ctx).First(&hook, "id = ?", id).Error; err != nil { return nil, err }
	return &hook, nil
}

// NewService creates a new webhook service
func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendWebhook sends a webhook notification to the specified URL
func (s *Service) SendWebhook(ctx context.Context, url string, payload WebhookPayload) error {
	if url == "" {
		return nil
	}

	logger.Info("Sending webhook", "job_id", payload.JobID, "url", url, "status", payload.Status)

	// Marshal payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Scriberr-Webhook/1.0")

	// Send request with retry logic
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second) // Simple backoff
			logger.Info("Retrying webhook", "job_id", payload.JobID, "attempt", i+1)
		}

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err
			logger.Warn("Webhook request failed", "error", err, "attempt", i+1)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Info("Webhook sent successfully", "job_id", payload.JobID, "status_code", resp.StatusCode)
			return nil
		}

		lastErr = fmt.Errorf("webhook returned non-success status: %d", resp.StatusCode)
		logger.Warn("Webhook returned error status", "status_code", resp.StatusCode, "attempt", i+1)
	}

	return fmt.Errorf("failed to send webhook after %d attempts: %w", maxRetries, lastErr)
}
