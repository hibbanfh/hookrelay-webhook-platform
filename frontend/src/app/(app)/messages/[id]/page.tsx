"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { JsonBlock } from "@/components/json-block";
import { DeliveryList } from "@/components/messages/delivery-list";
import { QueryState } from "@/components/query-state";
import { RelativeTime } from "@/components/relative-time";
import { link } from "@/components/ui";
import { asApiError } from "@/lib/api/errors";
import { useMessage } from "@/lib/queries/messages";

export default function MessageDetailPage() {
  const { id } = useParams<{ id: string }>();
  const message = useMessage(id);

  if (message.isError && asApiError(message.error).status === 404) {
    return (
      <section aria-labelledby="not-found-title">
        <h1 id="not-found-title" className="text-2xl font-semibold tracking-tight">
          Message not found
        </h1>
        <p className="mt-2 text-sm text-slate-600">
          It may belong to another project, or the link is wrong.{" "}
          <Link href="/messages" className={link}>
            Back to messages
          </Link>
        </p>
      </section>
    );
  }

  return (
    <section aria-labelledby="message-title" className="space-y-6">
      <p className="text-sm">
        <Link href="/messages" className={link}>
          &larr; Messages
        </Link>
      </p>
      <QueryState query={message} loadingLabel="Loading message…">
        {message.data && (
          <>
            <header>
              <h1 id="message-title" className="font-mono text-2xl font-semibold tracking-tight">
                {message.data.event_type}
              </h1>
              <p className="mt-1 text-sm text-slate-600">
                <span className="font-mono">{message.data.id}</span> &middot; published <RelativeTime iso={message.data.created_at} />
              </p>
            </header>
            <div>
              <h2 className="mb-2 text-base font-semibold">Payload</h2>
              <JsonBlock label="Payload" value={message.data.payload} />
            </div>
            <div>
              <h2 className="mb-2 text-base font-semibold">Deliveries</h2>
              <DeliveryList deliveries={message.data.deliveries} />
            </div>
          </>
        )}
      </QueryState>
    </section>
  );
}
