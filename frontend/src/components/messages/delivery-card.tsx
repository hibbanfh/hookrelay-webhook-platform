"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { LoadMoreButton } from "@/components/load-more-button";
import { QueryState } from "@/components/query-state";
import { RelativeTime } from "@/components/relative-time";
import type { Delivery } from "@/lib/api/types";
import { useAttempts } from "@/lib/queries/deliveries";
import { flattenPages } from "@/lib/queries/pages";
import { keys } from "@/lib/queries/keys";
import { AttemptTable } from "./attempt-table";
import { ReplayButton } from "./replay-button";
import { StatusBadge } from "./status-badge";

interface Props {
  delivery: Delivery;
  /** From the cached endpoints list; falls back to the endpoint ID. */
  endpointUrl: string | undefined;
}

export function DeliveryCard({ delivery, endpointUrl }: Props) {
  const queryClient = useQueryClient();
  const attempts = useAttempts(delivery.id);

  // When the delivery's status or attempt count changes, re-read the attempts once more. This closes
  // the race where the last poll landed just before the attempt was written.
  const seen = useRef(`${delivery.status}:${delivery.attempt_count}`);
  useEffect(() => {
    const now = `${delivery.status}:${delivery.attempt_count}`;
    if (seen.current === now) return;
    seen.current = now;
    void queryClient.invalidateQueries({ queryKey: keys.attempts(delivery.id) });
  }, [delivery.status, delivery.attempt_count, delivery.id, queryClient]);

  const items = flattenPages(attempts.data);
  const headingId = `delivery-${delivery.id}`;
  return (
    <article aria-labelledby={headingId} className="rounded-lg border border-slate-200 bg-white p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <h3 id={headingId} className="break-all font-mono text-sm font-medium">
            {endpointUrl ?? delivery.endpoint_id}
          </h3>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-600">
            <StatusBadge status={delivery.status} />
            <span>{delivery.attempt_count} attempt{delivery.attempt_count === 1 ? "" : "s"}</span>
            {delivery.last_attempt_at && (
              <span>
                last <RelativeTime iso={delivery.last_attempt_at} />
              </span>
            )}
          </p>
        </div>
        <ReplayButton messageId={delivery.message_id} deliveryId={delivery.id} status={delivery.status} />
      </div>
      <div className="mt-4">
        <QueryState query={attempts} loadingLabel="Loading attempts…">
          <AttemptTable attempts={items} />
          <LoadMoreButton
            hasNextPage={attempts.hasNextPage}
            isFetchingNextPage={attempts.isFetchingNextPage}
            fetchNextPage={attempts.fetchNextPage}
          />
        </QueryState>
      </div>
    </article>
  );
}
