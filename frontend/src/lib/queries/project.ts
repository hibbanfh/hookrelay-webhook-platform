"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { unwrap } from "@/lib/api/errors";
import type { Project } from "@/lib/api/types";
import { keys } from "./keys";

/** GET /v1/project. The demo receiver URL is deployment config, so it never goes stale. */
export function useProject() {
  return useQuery<Project>({
    queryKey: keys.project(),
    staleTime: Infinity,
    queryFn: async () => unwrap(await api.GET("/v1/project")),
  });
}
