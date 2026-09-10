package api

import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strings"

    "scriberr/internal/models"
    "scriberr/internal/webhook"
    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
)

type webhookRequest struct {
    Name string `json:"name" binding:"required"`
    URL string `json:"url" binding:"required"`
    Secret string `json:"secret,omitempty"`
    Events []webhook.Event `json:"events" binding:"required"`
    Enabled *bool `json:"enabled"`
}

type webhookResponse struct {
    ID string `json:"id"`
    Name string `json:"name"`
    URL string `json:"url"`
    Events []string `json:"events"`
    Enabled bool `json:"enabled"`
    HasSecret bool `json:"has_secret"`
}

func toWebhookResponse(h models.Webhook) webhookResponse {
    var events []string
    _ = json.Unmarshal([]byte(h.Events), &events)
    return webhookResponse{ID: h.ID, Name: h.Name, URL: h.URL, Events: events, Enabled: h.Enabled, HasSecret: h.Secret != nil && *h.Secret != ""}
}

func validateWebhookRequest(req webhookRequest) error {
    parsed, err := url.ParseRequestURI(req.URL)
    if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") { return fmt.Errorf("invalid webhook") }
    if len(req.Events) == 0 { return fmt.Errorf("invalid webhook") }
    valid := map[webhook.Event]bool{}; for _, event := range webhook.AllEvents { valid[event] = true }
    for _, event := range req.Events { if !valid[event] { return fmt.Errorf("invalid webhook") } }
    return nil
}

func (h *Handler) ListWebhooks(c *gin.Context) {
    hooks, err := h.webhookService.List(c.Request.Context()); if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    result := make([]webhookResponse, 0, len(hooks)); for _, hook := range hooks { result = append(result, toWebhookResponse(hook)) }
    c.JSON(http.StatusOK, result)
}

func (h *Handler) CreateWebhook(c *gin.Context) {
    var req webhookRequest; if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    if err := validateWebhookRequest(req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": "a valid HTTP(S) URL and at least one supported event are required"}); return }
    events, _ := json.Marshal(req.Events); enabled := true; if req.Enabled != nil { enabled = *req.Enabled }
    hook := &models.Webhook{ID: uuid.New().String(), Name: strings.TrimSpace(req.Name), URL: req.URL, Events: string(events), Enabled: enabled}; if req.Secret != "" { hook.Secret = &req.Secret }
    if err := h.webhookService.Create(c.Request.Context(), hook); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }; c.JSON(http.StatusCreated, toWebhookResponse(*hook))
}

func (h *Handler) UpdateWebhook(c *gin.Context) {
    hook, err := h.webhookService.Get(c.Request.Context(), c.Param("id")); if err != nil { c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"}); return }
    var req webhookRequest; if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    if err := validateWebhookRequest(req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": "a valid HTTP(S) URL and at least one supported event are required"}); return }
    events, _ := json.Marshal(req.Events); hook.Name = strings.TrimSpace(req.Name); hook.URL = req.URL; hook.Events = string(events); if req.Enabled != nil { hook.Enabled = *req.Enabled }; if req.Secret != "" { hook.Secret = &req.Secret }
    if err := h.webhookService.Update(c.Request.Context(), hook); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }; c.JSON(http.StatusOK, toWebhookResponse(*hook))
}

func (h *Handler) DeleteWebhook(c *gin.Context) { if err := h.webhookService.Delete(c.Request.Context(), c.Param("id")); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }; c.Status(http.StatusNoContent) }
