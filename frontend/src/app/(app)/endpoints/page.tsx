"use client";

import { skipToken, useQuery } from "@tanstack/react-query";
import { PROJECT_KEY, type ProjectInfo } from "@/lib/use-ensure-project";

// Placeholder shell page: US-008 builds the endpoints list.
export default function EndpointsPage() {
  // Reads the project the shell already ensured (no second request).
  const { data } = useQuery<ProjectInfo>({ queryKey: PROJECT_KEY, queryFn: skipToken });
  return (
    <section aria-labelledby="endpoints-title">
      <h1 id="endpoints-title" className="text-2xl font-semibold tracking-tight">Endpoints</h1>
      <p className="mt-2 text-slate-600">
        Project: <strong>{data?.name}</strong>. Endpoint management arrives with US-008.
      </p>
    </section>
  );
}
