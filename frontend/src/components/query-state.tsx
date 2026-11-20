"use client";

import type { ReactNode } from "react";
import { asApiError, displayMessage } from "@/lib/api/errors";
import { btn } from "@/components/ui";

interface QueryLike {
  isPending: boolean;
  isError: boolean;
  error: unknown;
  data?: unknown;
  refetch: () => unknown;
}

interface Props {
  query: QueryLike;
  /** Shown while loading, for screen readers and sighted users. */
  loadingLabel: string;
  children: ReactNode;
}

/** Loading and error (with Retry) states of a query. The caller renders empty and success in `children`. */
export function QueryState({ query, loadingLabel, children }: Props) {
  if (query.isPending) {
    return (
      <p role="status" className="text-sm text-slate-600">
        {loadingLabel}
      </p>
    );
  }
  const hasData = query.data !== undefined;
  if (query.isError && !hasData) {
    return (
      <div role="alert" className="flex flex-wrap items-center gap-3 rounded-md bg-red-50 p-4 text-sm text-red-800">
        <span>{displayMessage(asApiError(query.error))}</span>
        <button type="button" className={btn} onClick={() => void query.refetch()}>
          Retry
        </button>
      </div>
    );
  }
  return (
    <>
      {query.isError && (
        // Keep what is on screen; a background refetch failed and will be retried.
        <p role="status" className="mb-2 text-xs text-amber-800">
          Couldn&apos;t refresh, retrying
        </p>
      )}
      {children}
    </>
  );
}
