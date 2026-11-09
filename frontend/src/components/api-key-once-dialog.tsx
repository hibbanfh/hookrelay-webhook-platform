"use client";

import { useEffect, useRef, useState } from "react";

interface Props {
  apiKey: string;
  onClose: () => void;
}

/** Shows the API key exactly once. The key lives in props/state only and is never persisted. */
export function ApiKeyOnceDialog({ apiKey, onClose }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const [copied, setCopied] = useState<"idle" | "copied" | "failed">("idle");

  useEffect(() => {
    const dialog = ref.current;
    if (dialog && !dialog.open) dialog.showModal?.();
  }, []);

  async function copy() {
    try {
      await navigator.clipboard.writeText(apiKey);
      setCopied("copied");
    } catch {
      setCopied("failed");
    }
  }

  return (
    <dialog
      ref={ref}
      aria-labelledby="api-key-title"
      aria-describedby="api-key-warning"
      // Escape or backdrop must not silently discard a key the user has not saved: only the button closes it.
      onCancel={(e) => e.preventDefault()}
      className="m-auto w-[calc(100%-2rem)] max-w-lg rounded-lg border border-slate-200 bg-white p-6 shadow-xl backdrop:bg-slate-900/50"
    >
      <h2 id="api-key-title" className="text-lg font-semibold">
        Your API key
      </h2>
      <p id="api-key-warning" role="alert" className="mt-2 rounded-md bg-amber-50 p-3 text-sm text-amber-900">
        Copy it now. For your security it is shown only once and cannot be retrieved again.
      </p>
      <code
        data-testid="api-key"
        className="mt-4 block break-all rounded-md bg-slate-100 p-3 font-mono text-sm select-all"
      >
        {apiKey}
      </code>
      <div className="mt-4 flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={copy}
          className="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
        >
          Copy key
        </button>
        <span role="status" className="text-sm text-slate-600">
          {copied === "copied" && "Copied to clipboard"}
          {copied === "failed" && "Copy failed. Select the key and copy it manually."}
        </span>
        <button
          type="button"
          onClick={onClose}
          className="ml-auto rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
        >
          I&apos;ve saved it
        </button>
      </div>
    </dialog>
  );
}
