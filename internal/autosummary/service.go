package autosummary

import (
    "context"
    "fmt"
    "strings"
    "time"

    "scriberr/internal/llm"
    "scriberr/internal/models"
    "scriberr/internal/repository"
    "scriberr/pkg/logger"
    "gorm.io/gorm"
)

// Service generates summaries for completed transcriptions when enabled.
type Service struct {
    jobRepo repository.JobRepository
    summaryRepo repository.SummaryRepository
    llmRepo repository.LLMConfigRepository
}

func NewService(jobRepo repository.JobRepository, summaryRepo repository.SummaryRepository, llmRepo repository.LLMConfigRepository) *Service {
    return &Service{jobRepo: jobRepo, summaryRepo: summaryRepo, llmRepo: llmRepo}
}

func (s *Service) Process(ctx context.Context, jobID string) error {
    settings, err := s.summaryRepo.GetSettings(ctx)
    if err != nil { if err == gorm.ErrRecordNotFound { return nil }; return fmt.Errorf("load summary settings: %w", err) }
    if !settings.AutoSummarize || settings.DefaultTemplateID == nil || *settings.DefaultTemplateID == "" { return nil }

    job, err := s.jobRepo.FindWithAssociations(ctx, jobID)
    if err != nil { return fmt.Errorf("load transcription: %w", err) }
    if job.Transcript == nil || strings.TrimSpace(*job.Transcript) == "" { return fmt.Errorf("transcription has no transcript") }
    template, err := s.summaryRepo.FindByID(ctx, *settings.DefaultTemplateID)
    if err != nil { return fmt.Errorf("load default summary template: %w", err) }
    cfg, err := s.llmRepo.GetActive(ctx)
    if err != nil { return fmt.Errorf("load active LLM configuration: %w", err) }

    var service llm.Service
    switch strings.ToLower(cfg.Provider) {
    case "openai":
        if cfg.APIKey == nil || *cfg.APIKey == "" { return fmt.Errorf("OpenAI API key is not configured") }
        service = llm.NewOpenAIService(*cfg.APIKey, cfg.OpenAIBaseURL)
    case "ollama":
        if cfg.BaseURL == nil || *cfg.BaseURL == "" { return fmt.Errorf("Ollama base URL is not configured") }
        service = llm.NewOllamaService(*cfg.BaseURL)
    default:
        return fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
    }

    requestCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
    defer cancel()
    content := "Transcript:\n" + *job.Transcript + "\n\nInstructions:\n" + template.Prompt
    response, err := service.ChatCompletion(requestCtx, template.Model, []llm.ChatMessage{{Role: "user", Content: content}}, 0.0)
    if err != nil { return fmt.Errorf("generate summary: %w", err) }
    if response == nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" { return fmt.Errorf("LLM returned an empty summary") }

    summary := &models.Summary{TranscriptionID: job.ID, TemplateID: &template.ID, Model: template.Model, Content: response.Choices[0].Message.Content}
    if err := s.summaryRepo.SaveSummary(ctx, summary); err != nil { return fmt.Errorf("save summary: %w", err) }
    if err := s.jobRepo.UpdateSummary(ctx, job.ID, summary.Content); err != nil { logger.Warn("Auto-summary saved but job cache update failed", "job_id", job.ID, "error", err) }
    logger.Info("Automatic summary generated", "job_id", job.ID, "template_id", template.ID)
    return nil
}
