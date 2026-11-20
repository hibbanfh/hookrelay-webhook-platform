"use client";

import { EmptyState } from "@/components/empty-state";
import { LoadMoreButton } from "@/components/load-more-button";
import { MessageTable } from "@/components/messages/message-table";
import { PublishCurlExample } from "@/components/messages/publish-curl-example";
import { QueryState } from "@/components/query-state";
import { flattenPages } from "@/lib/queries/pages";
import { useMessages } from "@/lib/queries/messages";

export default function MessagesPage() {
  const messages = useMessages();
  const items = flattenPages(messages.data);
  return (
    <section aria-labelledby="messages-title" className="space-y-6">
      <h1 id="messages-title" className="text-2xl font-semibold tracking-tight">
        Messages
      </h1>
      <QueryState query={messages} loadingLabel="Loading messages…">
        {items.length === 0 ? (
          <EmptyState
            title="No messages yet"
            action={<PublishCurlExample />}
          >
            Publish your first message with your API key. It will show up here with its deliveries.
          </EmptyState>
        ) : (
          <>
            <MessageTable messages={items} />
            <LoadMoreButton
              hasNextPage={messages.hasNextPage}
              isFetchingNextPage={messages.isFetchingNextPage}
              fetchNextPage={messages.fetchNextPage}
            />
          </>
        )}
      </QueryState>
    </section>
  );
}
