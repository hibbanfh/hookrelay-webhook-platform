"use client";

import { EmptyState } from "@/components/empty-state";
import { RelativeTime } from "@/components/relative-time";
import { td, th } from "@/components/ui";
import type { Endpoint } from "@/lib/api/types";
import { EnabledToggle } from "./enabled-toggle";
import { SecretReveal } from "./secret-reveal";

export function EndpointTable({ endpoints }: { endpoints: Endpoint[] }) {
  if (endpoints.length === 0) {
    return (
      <EmptyState title="No endpoints yet">
        An endpoint is a URL that receives your webhooks. Add one above, or click &quot;Use demo receiver&quot; to try
        Hookrelay without running a server.
      </EmptyState>
    );
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
      <table className="w-full min-w-[40rem] border-collapse">
        <caption className="sr-only">Endpoints</caption>
        <thead className="border-b border-slate-200 bg-slate-50">
          <tr>
            <th scope="col" className={th}>URL</th>
            <th scope="col" className={th}>Description</th>
            <th scope="col" className={th}>Created</th>
            <th scope="col" className={th}>Status</th>
            <th scope="col" className={th}>Signing secret</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {endpoints.map((endpoint) => (
            <tr key={endpoint.id}>
              <td className={`${td} break-all font-mono text-xs`}>{endpoint.url}</td>
              <td className={td}>{endpoint.description || <span className="text-slate-500">None</span>}</td>
              <td className={td}><RelativeTime iso={endpoint.created_at} /></td>
              <td className={td}><EnabledToggle endpoint={endpoint} /></td>
              <td className={td}><SecretReveal endpointId={endpoint.id} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
