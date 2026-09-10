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

// WebhookRequest is the configuration accepted when creating or updating a webhook.
type WebhookRequest struct {
	Name    string          `json:"name" binding:"required"`
	URL     string          `json:"url" binding:"required"`
	Secret  string          `json:"secret,omitempty"`
	Events  []webhook.Event `json:"events" binding:"required"`
	Enabled *bool           `json:"enabled"`
}

// WebhookResponse describes a configured webhook without exposing its signing secret.
type WebhookResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	HasSecret bool     `json:"has_secret"`
}

func toWebhookResponse(h models.Webhook) WebhookResponse {
	var events []string
	_ = json.Unmarshal([]byte(h.Events), &events)
	return WebhookResponse{ID: h.ID, Name: h.Name, URL: h.URL, Events: events, Enabled: h.Enabled, HasSecret: h.Secret != nil && *h.Secret != ""}
}

func validateWebhookRequest(req WebhookRequest) error {
	parsed, err := url.ParseRequestURI(req.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid webhook")
	}
	if len(req.Events) == 0 {
		return fmt.Errorf("invalid webhook")
	}
	valid := map[webhook.Event]bool{}
	for _, event := range webhook.AllEvents {
		valid[event] = true
	}
	for _, event := range req.Events {
		if !valid[event] {
			return fmt.Errorf("invalid webhook")
		}
	}
	return nil
}

// ListWebhooks returns all configured webhook subscriptions.
// @Summary List webhooks
// @Description List configured outbound webhook subscriptions without exposing signing secrets
// @Tags webhooks
// @Produce json
// @Success 200 {array} WebhookResponse
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/webhooks/ [get]
func (h *Handler) ListWebhooks(c *gin.Context) {
	hooks, err := h.webhookService.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]WebhookResponse, 0, len(hooks))
	for _, hook := range hooks {
		result = append(result, toWebhookResponse(hook))
	}
	c.JSON(http.StatusOK, result)
}

// CreateWebhook creates an outbound webhook subscription.
// @Summary Create a webhook
// @Description Create an outbound webhook subscription for one or more recording lifecycle events
// @Tags webhooks
// @Accept json
// @Produce json
// @Param request body WebhookRequest true "Webhook configuration"
// @Success 201 {object} WebhookResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/webhooks/ [post]
func (h *Handler) CreateWebhook(c *gin.Context) {
	var req WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateWebhookRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid HTTP(S) URL and at least one supported event are required"})
		return
	}
	events, _ := json.Marshal(req.Events)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	hook := &models.Webhook{ID: uuid.New().String(), Name: strings.TrimSpace(req.Name), URL: req.URL, Events: string(events), Enabled: enabled}
	if req.Secret != "" {
		hook.Secret = &req.Secret
	}
	if err := h.webhookService.Create(c.Request.Context(), hook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toWebhookResponse(*hook))
}

// UpdateWebhook replaces an outbound webhook subscription's configuration.
// @Summary Update a webhook
// @Description Update a webhook's name, URL, subscribed events, enabled state, and optionally its signing secret
// @Tags webhooks
// @Accept json
// @Produce json
// @Param id path string true "Webhook ID"
// @Param request body WebhookRequest true "Webhook configuration"
// @Success 200 {object} WebhookResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/webhooks/{id} [put]
func (h *Handler) UpdateWebhook(c *gin.Context) {
	hook, err := h.webhookService.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}
	var req WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateWebhookRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid HTTP(S) URL and at least one supported event are required"})
		return
	}
	events, _ := json.Marshal(req.Events)
	hook.Name = strings.TrimSpace(req.Name)
	hook.URL = req.URL
	hook.Events = string(events)
	if req.Enabled != nil {
		hook.Enabled = *req.Enabled
	}
	if req.Secret != "" {
		hook.Secret = &req.Secret
	}
	if err := h.webhookService.Update(c.Request.Context(), hook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toWebhookResponse(*hook))
}

// DeleteWebhook removes an outbound webhook subscription.
// @Summary Delete a webhook
// @Description Delete a configured outbound webhook subscription
// @Tags webhooks
// @Param id path string true "Webhook ID"
// @Success 204
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/webhooks/{id} [delete]
func (h *Handler) DeleteWebhook(c *gin.Context) {
	if err := h.webhookService.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
