import Link from "next/link";
import { RelativeTime } from "@/components/relative-time";
import { link, td, th } from "@/components/ui";
import type { MessageSummary } from "@/lib/api/types";
import { DeliverySummary } from "./delivery-summary";

/** Rows keep the order the API returned (newest first). */
export function MessageTable({ messages }: { messages: MessageSummary[] }) {
  return (
    <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
      <table className="w-full min-w-[32rem] border-collapse">
        <caption className="sr-only">Messages, newest first</caption>
        <thead className="border-b border-slate-200 bg-slate-50">
          <tr>
            <th scope="col" className={th}>Event type</th>
            <th scope="col" className={th}>Published</th>
            <th scope="col" className={th}>Deliveries</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {messages.map((m) => (
            <tr key={m.id}>
              <td className={td}>
                <Link href={`/messages/${m.id}`} className={`${link} font-mono`}>
                  {m.event_type}
                </Link>
              </td>
              <td className={td}><RelativeTime iso={m.created_at} /></td>
              <td className={td}><DeliverySummary counts={m.delivery_summary} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
