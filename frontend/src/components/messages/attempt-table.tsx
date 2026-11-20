import { RelativeTime } from "@/components/relative-time";
import { td, th } from "@/components/ui";
import type { Attempt } from "@/lib/api/types";

function result(a: Attempt): string {
  if (a.response_status !== null) return String(a.response_status);
  return a.error ?? "no response";
}

export function AttemptTable({ attempts }: { attempts: Attempt[] }) {
  if (attempts.length === 0) return <p className="text-sm text-slate-600">No attempts yet</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[36rem] border-collapse">
        <caption className="sr-only">Delivery attempts</caption>
        <thead className="border-b border-slate-200">
          <tr>
            <th scope="col" className={th}>#</th>
            <th scope="col" className={th}>Started</th>
            <th scope="col" className={th}>Result</th>
            <th scope="col" className={th}>Duration</th>
            <th scope="col" className={th}>Trigger</th>
            <th scope="col" className={th}>Response body</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {attempts.map((a) => (
            <tr key={a.id} data-testid="attempt-row">
              <td className={td}>{a.attempt_number}</td>
              <td className={td}><RelativeTime iso={a.started_at} /></td>
              <td className={`${td} font-mono`}>{result(a)}</td>
              <td className={td}>{a.duration_ms} ms</td>
              <td className={td}>{a.trigger}</td>
              <td className={td}>
                {a.response_body ? (
                  // Untrusted text: React escapes it, it is never rendered as HTML.
                  <code className="block max-h-24 overflow-auto whitespace-pre-wrap break-all font-mono text-xs">{a.response_body}</code>
                ) : (
                  <span className="text-slate-500">{a.response_body === null ? "No response" : "Empty body"}</span>
                )}
                {a.response_body_truncated && (
                  <span className="ml-1 rounded bg-slate-200 px-1.5 py-0.5 text-xs">…truncated</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
