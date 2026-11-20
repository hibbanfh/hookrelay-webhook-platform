"use client";

import { CreateEndpointForm } from "@/components/endpoints/create-endpoint-form";
import { EndpointTable } from "@/components/endpoints/endpoint-table";
import { LoadMoreButton } from "@/components/load-more-button";
import { QueryState } from "@/components/query-state";
import { flattenPages } from "@/lib/queries/pages";
import { useEndpoints } from "@/lib/queries/endpoints";

export default function EndpointsPage() {
  const endpoints = useEndpoints();
  const items = flattenPages(endpoints.data);
  return (
    <section aria-labelledby="endpoints-title" className="space-y-6">
      <h1 id="endpoints-title" className="text-2xl font-semibold tracking-tight">
        Endpoints
      </h1>
      <CreateEndpointForm />
      <QueryState query={endpoints} loadingLabel="Loading endpoints…">
        <EndpointTable endpoints={items} />
        <LoadMoreButton
          hasNextPage={endpoints.hasNextPage}
          isFetchingNextPage={endpoints.isFetchingNextPage}
          fetchNextPage={endpoints.fetchNextPage}
        />
      </QueryState>
    </section>
  );
}
