import type { InfiniteData } from "@tanstack/react-query";

/** All items of every loaded page, in order. */
export function flattenPages<T, P>(data: InfiniteData<{ data: T[] }, P> | undefined): T[] {
  return data?.pages.flatMap((p) => p.data) ?? [];
}
