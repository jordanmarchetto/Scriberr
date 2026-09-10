import { useCallback, useEffect, useState } from "react";
import { Plus, Trash2, Webhook as WebhookIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { useAuth } from "@/features/auth/hooks/useAuth";

type Hook = { id: string; name: string; url: string; events: string[]; enabled: boolean; has_secret: boolean };
const events = [
  ["recording.uploaded", "Recording uploaded"],
  ["transcription.completed", "Transcription completed"],
  ["transcription.failed", "Transcription failed"],
  ["summary.completed", "Summary completed"],
  ["summary.failed", "Summary failed"],
] as const;

export function WebhookSettings() {
  const { getAuthHeaders } = useAuth();
  const [hooks, setHooks] = useState<Hook[]>([]);
  const [name, setName] = useState(""); const [url, setUrl] = useState(""); const [secret, setSecret] = useState("");
  const [selected, setSelected] = useState<string[]>(["recording.uploaded"]); const [error, setError] = useState("");
  const load = useCallback(async () => { const r = await fetch("/api/v1/webhooks/", { headers: getAuthHeaders() }); if (r.ok) setHooks(await r.json()); }, [getAuthHeaders]);
  useEffect(() => { load(); }, [load]);
  const toggle = (event: string) => setSelected((v) => v.includes(event) ? v.filter((x) => x !== event) : [...v, event]);
  const create = async () => {
    setError(""); if (!name.trim() || !url.trim() || selected.length === 0) { setError("Name, URL, and at least one event are required."); return; }
    const r = await fetch("/api/v1/webhooks/", { method: "POST", headers: { "Content-Type": "application/json", ...getAuthHeaders() }, body: JSON.stringify({ name, url, secret, events: selected, enabled: true }) });
    if (!r.ok) { setError((await r.json()).error || "Could not create webhook"); return; }
    setName(""); setUrl(""); setSecret(""); setSelected(["recording.uploaded"]); load();
  };
  const remove = async (id: string) => { if (!confirm("Delete this webhook?")) return; await fetch(`/api/v1/webhooks/${id}`, { method: "DELETE", headers: getAuthHeaders() }); load(); };
  return <div className="space-y-6">
    <div className="bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-[var(--radius-card)] p-4 sm:p-6 shadow-sm">
      <div className="flex items-center gap-3 mb-1"><WebhookIcon className="h-5 w-5 text-[var(--brand-solid)]" /><h3 className="text-lg font-medium text-[var(--text-primary)]">Webhooks</h3></div>
      <p className="text-sm text-[var(--text-secondary)] mb-5">Send recording lifecycle events to external services. Delivery is asynchronous.</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Webhook name" aria-label="Webhook name" />
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/webhook" aria-label="Webhook URL" type="url" />
        <Input value={secret} onChange={(e) => setSecret(e.target.value)} placeholder="Signing secret (optional)" aria-label="Signing secret" type="password" />
      </div>
      <div className="mt-4"><p className="text-sm font-medium text-[var(--text-primary)] mb-2">Fire when</p><div className="grid gap-2 sm:grid-cols-2">{events.map(([value, label]) => <label key={value} className="flex items-center gap-2 text-sm text-[var(--text-secondary)]"><Checkbox checked={selected.includes(value)} onCheckedChange={() => toggle(value)} />{label}</label>)}</div></div>
      {error && <p className="text-sm text-[var(--danger-solid)] mt-3">{error}</p>}
      <Button className="mt-5" onClick={create}><Plus className="h-4 w-4" /> Add webhook</Button>
    </div>
    <div className="space-y-3">{hooks.map((hook) => <div key={hook.id} className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-[var(--bg-card)] border border-[var(--border-subtle)] rounded-[var(--radius-card)] p-4"><div><p className="font-medium text-[var(--text-primary)]">{hook.name}</p><p className="text-sm text-[var(--text-secondary)] break-all">{hook.url}</p><p className="text-xs text-[var(--text-tertiary)] mt-1">{hook.events.join(" · ")}{hook.has_secret ? " · signed" : ""}</p></div><Button variant="ghost" size="icon" onClick={() => remove(hook.id)} aria-label={`Delete ${hook.name}`}><Trash2 className="h-4 w-4 text-[var(--danger-solid)]" /></Button></div>)}</div>
  </div>;
}
