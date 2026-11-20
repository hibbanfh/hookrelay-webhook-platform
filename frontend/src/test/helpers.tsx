import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { vi } from "vitest";

export function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 }, mutations: { retry: false } } });
}

export function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

export function renderWithClient(ui: ReactElement, client = makeClient()) {
  return { client, ...render(ui, { wrapper: wrapper(client) }) };
}

export interface MockCall {
  method: string;
  /** Path and query string, without the /api prefix. */
  url: string;
  body: string;
}
export interface MockReply {
  status?: number;
  body?: unknown;
}
export type Handler = (call: MockCall) => MockReply | Promise<MockReply>;

/** Stubs globalThis.fetch with a router keyed by "METHOD /v1/path" (no query string). */
export function mockApi(routes: Record<string, Handler | MockReply>) {
  const calls: MockCall[] = [];
  const fetchMock = vi.fn(async (input: Request) => {
    const url = new URL(input.url);
    const path = url.pathname.replace(/^\/api/, "");
    const call: MockCall = { method: input.method, url: `${path}${url.search}`, body: await input.clone().text() };
    calls.push(call);
    const route = routes[`${call.method} ${path}`];
    if (!route) return new Response(JSON.stringify({ error: { code: "not_found", message: "no mock" } }), { status: 404 });
    const reply = typeof route === "function" ? await route(call) : route;
    return new Response(reply.body === undefined ? null : JSON.stringify(reply.body), {
      status: reply.status ?? 200,
      headers: { "content-type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  return {
    calls,
    fetchMock,
    count: (method: string, path: string) => calls.filter((c) => c.method === method && c.url.split("?")[0] === path).length,
  };
}

export const apiError = (status: number, code: string, message: string, field?: string): MockReply => ({
  status,
  body: { error: { code, message, ...(field ? { field } : {}) } },
});

export const NOW = "2026-11-20T10:00:00Z";
