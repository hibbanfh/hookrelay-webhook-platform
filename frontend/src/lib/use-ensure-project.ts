"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { api, REPROVISION_EVENT } from "@/lib/api/client";

export const PROJECT_KEY = ["me", "project"] as const;

export interface ProjectInfo {
  id: string;
  name: string;
}

/**
 * Runs POST /v1/me/project before anything else. A 201 carries the API key once: it is kept in
 * React state only (never in the query cache or any storage) until `dismissKey` is called.
 */
export function useEnsureProject() {
  const queryClient = useQueryClient();
  const [apiKey, setApiKey] = useState<string | null>(null);

  const query = useQuery({
    queryKey: PROJECT_KEY,
    staleTime: Infinity,
    queryFn: async (): Promise<ProjectInfo> => {
      const { data, error, response } = await api.POST("/v1/me/project");
      if (!data) throw new Error(error ? `ensure project failed (${response.status})` : "ensure project failed");
      if ("api_key" in data) setApiKey(data.api_key);
      return { id: data.id, name: data.name };
    },
  });

  useEffect(() => {
    const again = () => void queryClient.invalidateQueries({ queryKey: PROJECT_KEY });
    window.addEventListener(REPROVISION_EVENT, again);
    return () => window.removeEventListener(REPROVISION_EVENT, again);
  }, [queryClient]);

  const dismissKey = useCallback(() => setApiKey(null), []);
  return { ...query, ensured: query.isSuccess, apiKey, dismissKey };
}
