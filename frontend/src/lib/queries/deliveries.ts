"use client";

import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { asApiError, unwrap } from "@/lib/api/errors";
import type { AttemptList, Delivery, MessageDetail } from "@/lib/api/types";
import { keys } from "./keys";

/**
 * No polling of its own: the message detail polls (see `detailPollInterval`) and `DeliveryCard` invalidates
 * this key whenever the delivery's status or attempt count changes.
 */
export function useAttempts(deliveryId: string) {
  return useInfiniteQuery({
    queryKey: keys.attempts(deliveryId),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }): Promise<AttemptList> =>
      unwrap(
        await api.GET("/v1/deliveries/{delivery_id}/attempts", {
          params: { path: { delivery_id: deliveryId }, query: { limit: 100, cursor: pageParam } },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** Replay: the 202 delivery (pending) is written into the message cache, so the button disables and polling starts at once. */
export function useReplayDelivery(messageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (deliveryId: string): Promise<Delivery> =>
      unwrap(await api.POST("/v1/deliveries/{delivery_id}/replay", { params: { path: { delivery_id: deliveryId } } })),
    onSuccess: (delivery) => {
      queryClient.setQueryData<MessageDetail>(keys.message(messageId), (old) =>
        old && { ...old, deliveries: old.deliveries.map((d) => (d.id === delivery.id ? delivery : d)) },
      );
      void queryClient.invalidateQueries({ queryKey: keys.messagesList() });
    },
    onError: (err) => {
      // The server state was different from what we showed (for example already pending).
      if (asApiError(err).status === 409) void queryClient.invalidateQueries({ queryKey: keys.message(messageId) });
    },
  });
}
