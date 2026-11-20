"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { asApiError, unwrap } from "@/lib/api/errors";
import type { Delivery, MessageDetail, MessageList } from "@/lib/api/types";
import { keys } from "./keys";

export const MESSAGES_POLL_MS = 5_000;
export const DETAIL_POLL_MIN_MS = 1_000;
// Short enough that a manual attempt still shows within the 10 s target: an attempt in flight reports its
// lease expiry (about 30 s away) as next_attempt_at, so a longer cap would hide a slow attempt finishing.
export const DETAIL_POLL_MAX_MS = 5_000;

/**
 * How long to wait before re-reading a message. `false` when nothing is pending. 1 s while a pending delivery
 * is due now (no `next_attempt_at`, or one in the past: first attempt, replay). Otherwise the time until the
 * earliest scheduled retry, clamped to [1 s, 5 s].
 */
export function detailPollInterval(deliveries: Pick<Delivery, "status" | "next_attempt_at">[], now: number): number | false {
  const pending = deliveries.filter((d) => d.status === "pending");
  if (pending.length === 0) return false;
  const due = pending.map((d) => (d.next_attempt_at === null ? now : Date.parse(d.next_attempt_at)));
  const wait = Math.min(...due) - now;
  return Math.min(Math.max(wait, DETAIL_POLL_MIN_MS), DETAIL_POLL_MAX_MS);
}

/** Polls while any loaded message still has pending deliveries; a refetch re-reads every loaded page. */
export function useMessages() {
  return useInfiniteQuery({
    queryKey: keys.messagesList(),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }): Promise<MessageList> =>
      unwrap(await api.GET("/v1/messages", { params: { query: { limit: 20, cursor: pageParam } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    refetchInterval: (query) =>
      query.state.data?.pages.some((p) => p.data.some((m) => m.delivery_summary.pending > 0)) ? MESSAGES_POLL_MS : false,
  });
}

/** Polls while any delivery is pending, at the interval `detailPollInterval` picks (1-5 s). */
export function useMessage(id: string) {
  return useQuery<MessageDetail>({
    queryKey: keys.message(id),
    queryFn: async () => unwrap(await api.GET("/v1/messages/{message_id}", { params: { path: { message_id: id } } })),
    // A 404 is an answer, not a transient failure.
    retry: (count, err) => asApiError(err).status !== 404 && count < 1,
    refetchInterval: (query) => (query.state.data ? detailPollInterval(query.state.data.deliveries, Date.now()) : false),
  });
}
