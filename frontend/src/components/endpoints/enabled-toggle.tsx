"use client";

import { focusRing } from "@/components/ui";
import { asApiError, displayMessage } from "@/lib/api/errors";
import type { Endpoint } from "@/lib/api/types";
import { useUpdateEndpoint } from "@/lib/queries/endpoints";

/** A switch bound to the server state: it flips only after the PATCH succeeds, so a failure needs no rollback. */
export function EnabledToggle({ endpoint }: { endpoint: Endpoint }) {
  const update = useUpdateEndpoint();
  return (
    <div className="flex flex-col items-start gap-1">
      <button
        type="button"
        role="switch"
        aria-checked={endpoint.enabled}
        aria-label={`Enabled: ${endpoint.url}`}
        disabled={update.isPending}
        onClick={() => update.mutate({ id: endpoint.id, enabled: !endpoint.enabled })}
        className={`inline-flex items-center gap-2 rounded-full disabled:opacity-60 ${focusRing}`}
      >
        <span
          aria-hidden="true"
          className={`relative inline-block h-5 w-9 rounded-full transition-colors ${endpoint.enabled ? "bg-indigo-600" : "bg-slate-400"}`}
        >
          <span
            className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all ${endpoint.enabled ? "left-4.5" : "left-0.5"}`}
          />
        </span>
        <span className="text-sm">{endpoint.enabled ? "Enabled" : "Disabled"}</span>
      </button>
      {update.isError && (
        <p role="alert" className="text-xs text-red-700">
          {displayMessage(asApiError(update.error))}
        </p>
      )}
    </div>
  );
}
