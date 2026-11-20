import { CopyButton } from "@/components/copy-button";
import { focusRing } from "@/components/ui";

// Inlined at build time (NEXT_PUBLIC_*). Public URL of the API, as a developer's machine would reach it.
const BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080";

export function publishCurl(baseUrl: string): string {
  return [
    `curl -X POST ${baseUrl}/v1/messages \\`,
    `  -H "Authorization: Bearer $HOOKRELAY_API_KEY" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -d '{"event_type":"invoice.paid","payload":{"id":"inv_1","amount":1200}}'`,
  ].join("\n");
}

export function PublishCurlExample() {
  const command = publishCurl(BASE_URL);
  return (
    <div>
      <pre tabIndex={0} aria-label="curl example" className={`overflow-x-auto rounded-md bg-slate-900 p-4 font-mono text-xs text-slate-100 ${focusRing}`}>
        {command}
      </pre>
      <div className="mt-2 flex items-center gap-2">
        <CopyButton value={command} label="Copy command" />
      </div>
    </div>
  );
}
