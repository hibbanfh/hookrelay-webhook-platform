"use client";

import { useInfiniteQuery, useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { unwrap } from "@/lib/api/errors";
import type { Endpoint, EndpointList } from "@/lib/api/types";
import { keys } from "./keys";

export function useEndpoints() {
  return useInfiniteQuery({
    queryKey: keys.endpointsList(),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }): Promise<EndpointList> =>
      unwrap(await api.GET("/v1/endpoints", { params: { query: { limit: 100, cursor: pageParam } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

function pickEndpoint(e: Endpoint): Endpoint {
  return { id: e.id, url: e.url, description: e.description, enabled: e.enabled, created_at: e.created_at, updated_at: e.updated_at };
}

export interface CreateEndpointInput {
  url: string;
  description: string;
}

export function useCreateEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: CreateEndpointInput): Promise<Endpoint> => {
      const created = unwrap(await api.POST("/v1/endpoints", { body: input }));
      // The signing secret must not enter any cache: it is read on demand with "Reveal secret".
      return pickEndpoint(created);
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.endpointsList() }),
  });
}

export function useUpdateEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { id: string; enabled: boolean }): Promise<Endpoint> =>
      unwrap(await api.PATCH("/v1/endpoints/{endpoint_id}", { params: { path: { endpoint_id: input.id } }, body: { enabled: input.enabled } })),
    onSuccess: async (endpoint) => {
      // Show the persisted state at once, then confirm with the server.
      queryClient.setQueryData<InfiniteData<EndpointList, string | undefined>>(keys.endpointsList(), (old) =>
        old && {
          ...old,
          pages: old.pages.map((page) => ({ ...page, data: page.data.map((e) => (e.id === endpoint.id ? endpoint : e)) })),
        },
      );
      await queryClient.invalidateQueries({ queryKey: keys.endpointsList() });
    },
  });
}

/**
 * Fetches a signing secret. Deliberately NOT a query or mutation: the value is returned to the
 * caller's component state only and never stored in the TanStack caches.
 */
export async function fetchSigningSecret(endpointId: string): Promise<string> {
  const res = unwrap(await api.GET("/v1/endpoints/{endpoint_id}/secret", { params: { path: { endpoint_id: endpointId } } }));
  return res.signing_secret;
}
