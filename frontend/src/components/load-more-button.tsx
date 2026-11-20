"use client";

import { btn } from "@/components/ui";

interface Props {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => unknown;
}

export function LoadMoreButton({ hasNextPage, isFetchingNextPage, fetchNextPage }: Props) {
  if (!hasNextPage) return null;
  return (
    <div className="mt-4">
      <button type="button" className={btn} disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
        {isFetchingNextPage ? "Loading…" : "Load more"}
      </button>
    </div>
  );
}
