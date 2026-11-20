"use client";

import { useMemo } from "react";
import type { Delivery } from "@/lib/api/types";
import { flattenPages } from "@/lib/queries/pages";
import { useEndpoints } from "@/lib/queries/endpoints";
import { NO_DELIVERIES_TEXT } from "./delivery-summary";
import { DeliveryCard } from "./delivery-card";

export function DeliveryList({ deliveries }: { deliveries: Delivery[] }) {
  const endpoints = useEndpoints();
  const urls = useMemo(
    () => new Map(flattenPages(endpoints.data).map((e) => [e.id, e.url])),
    [endpoints.data],
  );
  if (deliveries.length === 0) {
    return <p className="rounded-md border border-dashed border-slate-300 bg-white p-4 text-sm text-slate-600">{NO_DELIVERIES_TEXT}.</p>;
  }
  return (
    <ul className="space-y-4">
      {deliveries.map((d) => (
        <li key={d.id}>
          <DeliveryCard delivery={d} endpointUrl={urls.get(d.endpoint_id)} />
        </li>
      ))}
    </ul>
  );
}
