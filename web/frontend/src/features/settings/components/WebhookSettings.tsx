import { useCallback, useEffect, useState } from "react";
import {
  Pencil,
  Plus,
  RotateCcw,
  Save,
  Trash2,
  Webhook as WebhookIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { useAuth } from "@/features/auth/hooks/useAuth";

type WebhookSubscription = {
  id: string;
  name: string;
  url: string;
  events: string[];
  enabled: boolean;
  has_secret: boolean;
};

type WebhookDelivery = {
  id: string;
  webhook_id: string;
  webhook_name: string;
  event: string;
  job_id: string;
  status: "pending" | "processing" | "succeeded" | "failed";
  attempt_count: number;
  response_status?: number;
  last_error?: string;
  next_attempt_at?: string;
  delivered_at?: string;
  created_at: string;
};

type WebhookForm = {
  name: string;
  url: string;
  secret: string;
  events: string[];
  enabled: boolean;
  clearSecret: boolean;
};

const eventOptions = [
  ["recording.uploaded", "Recording uploaded"],
  ["transcription.completed", "Transcription completed"],
  ["transcription.failed", "Transcription failed"],
  ["summary.completed", "Summary completed"],
  ["summary.failed", "Summary failed"],
] as const;

const emptyForm = (): WebhookForm => ({
  name: "",
  url: "",
  secret: "",
  events: ["recording.uploaded"],
  enabled: true,
  clearSecret: false,
});

export function WebhookSettings() {
  const { getAuthHeaders } = useAuth();
  const [hooks, setHooks] = useState<WebhookSubscription[]>([]);
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [editingID, setEditingID] = useState<string | null>(null);
  const [form, setForm] = useState<WebhookForm>(emptyForm);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    const headers = getAuthHeaders();
    const [hooksResponse, deliveriesResponse] = await Promise.all([
      fetch("/api/v1/webhooks/", { headers }),
      fetch("/api/v1/webhooks/deliveries", { headers }),
    ]);
    if (hooksResponse.ok) {
      setHooks(await hooksResponse.json());
    }
    if (deliveriesResponse.ok) {
      setDeliveries(await deliveriesResponse.json());
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedHook = editingID
    ? hooks.find((hook) => hook.id === editingID)
    : undefined;

  const updateForm = <K extends keyof WebhookForm,>(
    key: K,
    value: WebhookForm[K],
  ) => setForm((current) => ({ ...current, [key]: value }));

  const toggleEvent = (event: string) => {
    updateForm(
      "events",
      form.events.includes(event)
        ? form.events.filter((configured) => configured !== event)
        : [...form.events, event],
    );
  };

  const resetForm = () => {
    setEditingID(null);
    setForm(emptyForm());
    setError("");
  };

  const beginEdit = (hook: WebhookSubscription) => {
    setEditingID(hook.id);
    setForm({
      name: hook.name,
      url: hook.url,
      secret: "",
      events: hook.events,
      enabled: hook.enabled,
      clearSecret: false,
    });
    setError("");
  };

  const save = async () => {
    setError("");
    if (!form.name.trim() || !form.url.trim() || form.events.length === 0) {
      setError("Name, URL, and at least one event are required.");
      return;
    }

    const response = await fetch(
      editingID ? `/api/v1/webhooks/${editingID}` : "/api/v1/webhooks/",
      {
        method: editingID ? "PUT" : "POST",
        headers: { "Content-Type": "application/json", ...getAuthHeaders() },
        body: JSON.stringify({
          name: form.name,
          url: form.url,
          secret: form.secret,
          clear_secret: form.clearSecret,
          events: form.events,
          enabled: form.enabled,
        }),
      },
    );
    if (!response.ok) {
      const body: { error?: string } = await response.json();
      setError(body.error || "Could not save webhook");
      return;
    }

    resetForm();
    await load();
  };

  const remove = async (hook: WebhookSubscription) => {
    if (!confirm(`Delete ${hook.name}? Delivery history will be retained.`)) {
      return;
    }
    const response = await fetch(`/api/v1/webhooks/${hook.id}`, {
      method: "DELETE",
      headers: getAuthHeaders(),
    });
    if (!response.ok) {
      setError("Could not delete webhook");
      return;
    }
    if (editingID === hook.id) {
      resetForm();
    }
    await load();
  };

  return (
    <div className="space-y-6">
      <div className="bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-[var(--radius-card)] p-4 sm:p-6 shadow-sm">
        <div className="flex items-center gap-3 mb-1">
          <WebhookIcon className="h-5 w-5 text-[var(--brand-solid)]" />
          <h3 className="text-lg font-medium text-[var(--text-primary)]">
            {editingID ? "Edit webhook" : "Webhooks"}
          </h3>
        </div>
        <p className="text-sm text-[var(--text-secondary)] mb-5">
          Events are queued before delivery and retried after temporary failures.
        </p>

        <div className="grid gap-3 sm:grid-cols-2">
          <Input
            value={form.name}
            onChange={(event) => updateForm("name", event.target.value)}
            placeholder="Webhook name"
            aria-label="Webhook name"
          />
          <Input
            value={form.url}
            onChange={(event) => updateForm("url", event.target.value)}
            placeholder="https://example.com/webhook"
            aria-label="Webhook URL"
            type="url"
          />
          <Input
            value={form.secret}
            onChange={(event) => {
              updateForm("secret", event.target.value);
              updateForm("clearSecret", false);
            }}
            placeholder={
              selectedHook?.has_secret
                ? "Leave blank to keep the current secret"
                : "Signing secret (optional)"
            }
            aria-label="Signing secret"
            type="password"
          />
          <label className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
            <Checkbox
              checked={form.enabled}
              onCheckedChange={(checked) =>
                updateForm("enabled", checked === true)
              }
            />
            Enabled
          </label>
        </div>

        {selectedHook?.has_secret && (
          <div className="flex items-center gap-3 mt-2">
            <p className="text-xs text-[var(--text-tertiary)]">
              {form.clearSecret
                ? "The existing signing secret will be removed."
                : "A signing secret is currently configured."}
            </p>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => updateForm("clearSecret", !form.clearSecret)}
            >
              {form.clearSecret ? "Keep secret" : "Remove secret"}
            </Button>
          </div>
        )}

        <div className="mt-4">
          <p className="text-sm font-medium text-[var(--text-primary)] mb-2">
            Fire when
          </p>
          <div className="grid gap-2 sm:grid-cols-2">
            {eventOptions.map(([value, label]) => (
              <label
                key={value}
                className="flex items-center gap-2 text-sm text-[var(--text-secondary)]"
              >
                <Checkbox
                  checked={form.events.includes(value)}
                  onCheckedChange={() => toggleEvent(value)}
                />
                {label}
              </label>
            ))}
          </div>
        </div>

        {error && (
          <p className="text-sm text-[var(--danger-solid)] mt-3">{error}</p>
        )}
        <div className="flex gap-2 mt-5">
          <Button onClick={save}>
            {editingID ? (
              <Save className="h-4 w-4" />
            ) : (
              <Plus className="h-4 w-4" />
            )}
            {editingID ? "Save changes" : "Add webhook"}
          </Button>
          {editingID && (
            <Button variant="outline" onClick={resetForm}>
              Cancel
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-3">
        {hooks.map((hook) => (
          <div
            key={hook.id}
            className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-[var(--radius-card)] p-4"
          >
            <div>
              <p className="font-medium text-[var(--text-primary)]">
                {hook.name}
                {!hook.enabled && (
                  <span className="text-xs text-[var(--text-tertiary)] ml-2">
                    disabled
                  </span>
                )}
              </p>
              <p className="text-sm text-[var(--text-secondary)] break-all">
                {hook.url}
              </p>
              <p className="text-xs text-[var(--text-tertiary)] mt-1">
                {hook.events.join(" · ")}
                {hook.has_secret ? " · signed" : ""}
              </p>
            </div>
            <div className="flex gap-1">
              <Button
                variant="ghost"
                size="icon"
                onClick={() => beginEdit(hook)}
                aria-label={`Edit ${hook.name}`}
              >
                <Pencil className="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => remove(hook)}
                aria-label={`Delete ${hook.name}`}
              >
                <Trash2 className="h-4 w-4 text-[var(--danger-solid)]" />
              </Button>
            </div>
          </div>
        ))}
      </div>

      <div className="bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-[var(--radius-card)] p-4 sm:p-6 shadow-sm">
        <div className="flex items-center justify-between gap-3 mb-4">
          <div>
            <h3 className="text-lg font-medium text-[var(--text-primary)]">
              Recent deliveries
            </h3>
            <p className="text-sm text-[var(--text-secondary)]">
              The latest 50 queued webhook events.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={() => void load()} aria-label="Refresh deliveries">
            <RotateCcw className="h-4 w-4" />
          </Button>
        </div>
        <div className="space-y-3">
          {deliveries.length === 0 && (
            <p className="text-sm text-[var(--text-tertiary)]">
              No webhook deliveries yet.
            </p>
          )}
          {deliveries.map((delivery) => (
            <div
              key={delivery.id}
              className="border border-[var(--border-subtle)] rounded-[var(--radius-btn)] p-3"
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-medium text-[var(--text-primary)]">
                  {delivery.webhook_name} · {delivery.event}
                </p>
                <span className="text-xs text-[var(--text-secondary)]">
                  {delivery.status} · {delivery.attempt_count} attempt
                  {delivery.attempt_count === 1 ? "" : "s"}
                  {delivery.response_status
                    ? ` · HTTP ${delivery.response_status}`
                    : ""}
                </span>
              </div>
              <p className="text-xs text-[var(--text-tertiary)] mt-1">
                Job {delivery.job_id} · {new Date(delivery.created_at).toLocaleString()}
              </p>
              {delivery.last_error && (
                <p className="text-xs text-[var(--danger-solid)] mt-1 break-words">
                  {delivery.last_error}
                </p>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
