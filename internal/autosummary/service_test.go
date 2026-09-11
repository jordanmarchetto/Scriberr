package autosummary

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scriberr/internal/models"
	"scriberr/internal/repository"
	transcriptioninterfaces "scriberr/internal/transcription/interfaces"
	"scriberr/internal/webhook"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newAutoSummaryTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:auto-summary-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&models.SummarySetting{}, &models.SummaryTemplate{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return &Service{summaryRepo: repository.NewSummaryRepository(db)}, db
}

func TestProcessDoesNothingWithoutSettings(t *testing.T) {
	service, _ := newAutoSummaryTestService(t)

	if err := service.Process(context.Background(), "job-123"); err != nil {
		t.Fatalf("Process returned an error: %v", err)
	}
}

func TestProcessDoesNothingWhenDisabled(t *testing.T) {
	service, db := newAutoSummaryTestService(t)
	if err := db.Create(&models.SummarySetting{AutoSummarize: false}).Error; err != nil {
		t.Fatalf("create summary settings: %v", err)
	}

	if err := service.Process(context.Background(), "job-123"); err != nil {
		t.Fatalf("Process returned an error: %v", err)
	}
}

func TestProcessDispatchesCompletedSummaryWebhook(t *testing.T) {
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("LLM request path = %q, want /api/chat", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"model":"test-model","message":{"role":"assistant","content":"Generated summary"},"done":true}`)
	}))
	defer llmServer.Close()

	received := make(chan webhook.EventPayload, 1)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload webhook.EventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode webhook payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhookServer.Close()

	dsn := fmt.Sprintf("file:auto-summary-webhook-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(
		&models.TranscriptionJob{},
		&models.MultiTrackFile{},
		&models.SummarySetting{},
		&models.SummaryTemplate{},
		&models.Summary{},
		&models.LLMConfig{},
		&models.SpeakerMapping{},
		&models.Webhook{},
		&models.WebhookDelivery{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	transcript := marshalTranscript(t, transcriptioninterfaces.TranscriptResult{Text: "A useful transcript."})
	job := models.TranscriptionJob{Status: models.StatusCompleted, AudioPath: "/tmp/audio.mp3", Transcript: &transcript}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create transcription job: %v", err)
	}
	template := models.SummaryTemplate{Name: "Test template", Model: "test-model", Prompt: "Summarize this."}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create summary template: %v", err)
	}
	if err := db.Create(&models.SummarySetting{AutoSummarize: true, DefaultTemplateID: &template.ID}).Error; err != nil {
		t.Fatalf("create summary settings: %v", err)
	}
	baseURL := llmServer.URL
	if err := db.Create(&models.LLMConfig{Provider: "ollama", BaseURL: &baseURL, IsActive: true}).Error; err != nil {
		t.Fatalf("create LLM config: %v", err)
	}
	if err := db.Create(&models.Webhook{Name: "summary receiver", URL: webhookServer.URL, Events: `["summary.completed"]`, Enabled: true}).Error; err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	webhookService := webhook.NewService()
	webhookService.SetDatabase(db)
	service := NewService(
		repository.NewJobRepository(db),
		repository.NewSummaryRepository(db),
		repository.NewLLMConfigRepository(db),
		repository.NewSpeakerMappingRepository(db),
	)
	service.SetWebhookDispatcher(webhookService)
	if err := service.Process(context.Background(), job.ID); err != nil {
		t.Fatalf("Process returned an error: %v", err)
	}

	select {
	case payload := <-received:
		if payload.Event != webhook.EventSummarySuccess {
			t.Errorf("event = %q, want %q", payload.Event, webhook.EventSummarySuccess)
		}
		if payload.Summary == nil || *payload.Summary != "Generated summary" {
			t.Errorf("summary = %v, want Generated summary", payload.Summary)
		}
		if payload.Metadata["model"] != "test-model" {
			t.Errorf("model metadata = %v, want test-model", payload.Metadata["model"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for summary.completed webhook")
	}
}

func TestBuildSummaryContentUsesPlainTranscriptText(t *testing.T) {
	transcriptJSON := marshalTranscript(t, transcriptioninterfaces.TranscriptResult{
		Text: "Hello from the transcript.",
	})

	content, err := buildSummaryContent(transcriptJSON, "Summarize this.", false, nil)
	if err != nil {
		t.Fatalf("buildSummaryContent returned an error: %v", err)
	}

	want := "Transcript:\nHello from the transcript.\n\nInstructions:\nSummarize this."
	if content != want {
		t.Fatalf("buildSummaryContent() = %q, want %q", content, want)
	}
}

func TestBuildSummaryContentUsesSpeakerLabelsAndMappings(t *testing.T) {
	speakerOne := "SPEAKER_00"
	speakerTwo := "SPEAKER_01"
	transcriptJSON := marshalTranscript(t, transcriptioninterfaces.TranscriptResult{
		Text: "Hello. Hi.",
		Segments: []transcriptioninterfaces.TranscriptSegment{
			{Text: " Hello. ", Speaker: &speakerOne},
			{Text: "Hi.", Speaker: &speakerTwo},
		},
	})
	mappings := []models.SpeakerMapping{
		{OriginalSpeaker: speakerOne, CustomName: "Jordan"},
	}

	content, err := buildSummaryContent(transcriptJSON, "Summarize this.", true, mappings)
	if err != nil {
		t.Fatalf("buildSummaryContent returned an error: %v", err)
	}

	want := "Transcript (with speaker labels - each line is prefixed with [SPEAKER_NAME]):\n" +
		"[Jordan] Hello.\n[SPEAKER_01] Hi.\n\nInstructions:\nSummarize this."
	if content != want {
		t.Fatalf("buildSummaryContent() = %q, want %q", content, want)
	}
}

func TestBuildSummaryContentRejectsInvalidTranscriptJSON(t *testing.T) {
	if _, err := buildSummaryContent("not JSON", "Summarize this.", false, nil); err == nil {
		t.Fatal("buildSummaryContent returned no error for invalid transcript JSON")
	}
}

func marshalTranscript(t *testing.T, transcript transcriptioninterfaces.TranscriptResult) string {
	t.Helper()

	data, err := json.Marshal(transcript)
	if err != nil {
		t.Fatalf("marshal transcript: %v", err)
	}
	return string(data)
}
