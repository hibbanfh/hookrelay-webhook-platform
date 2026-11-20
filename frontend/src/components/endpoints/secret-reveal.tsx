"use client";

import { useState } from "react";
import { CopyButton } from "@/components/copy-button";
import { btn } from "@/components/ui";
import { asApiError, displayMessage } from "@/lib/api/errors";
import { fetchSigningSecret } from "@/lib/queries/endpoints";

/**
 * "Reveal secret" for one endpoint. The secret lives in this component's state only: it is not in the
 * query cache or any storage, and "Hide" (or unmount) drops it.
 */
export function SecretReveal({ endpointId }: { endpointId: string }) {
  const [secret, setSecret] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function reveal() {
    setLoading(true);
    setError(null);
    try {
      setSecret(await fetchSigningSecret(endpointId));
    } catch (err) {
      setError(displayMessage(asApiError(err)));
    } finally {
      setLoading(false);
    }
  }

  if (secret === null) {
    return (
      <div className="flex flex-col items-start gap-1">
        <button type="button" className={btn} disabled={loading} onClick={() => void reveal()}>
          {loading ? "Revealing…" : "Reveal secret"}
        </button>
        {error && (
          <p role="alert" className="text-xs text-red-700">
            {error}
          </p>
        )}
      </div>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-2">
      <code data-testid="signing-secret" className="break-all rounded bg-slate-100 px-2 py-1 font-mono text-xs select-all">
        {secret}
      </code>
      <CopyButton value={secret} label="Copy secret" />
      <button type="button" className={btn} onClick={() => setSecret(null)}>
        Hide
      </button>
    </div>
  );
}
