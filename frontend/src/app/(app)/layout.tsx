"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { ApiKeyOnceDialog } from "@/components/api-key-once-dialog";
import { useEnsureProject } from "@/lib/use-ensure-project";

export default function AppLayout({ children }: { children: ReactNode }) {
  const project = useEnsureProject();
  const queryClient = useQueryClient();
  const [signingOut, setSigningOut] = useState(false);

  async function signOut() {
    setSigningOut(true);
    await fetch("/auth/sign-out", { method: "POST" }).catch(() => undefined);
    queryClient.clear();
    window.location.assign("/sign-in");
  }

  return (
    <div className="min-h-screen">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <span className="font-semibold">Hookrelay</span>
          <nav aria-label="Main" className="flex gap-4 text-sm">
            <Link href="/endpoints" className="hover:underline">Endpoints</Link>
            <Link href="/messages" className="hover:underline">Messages</Link>
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <span data-testid="project-name" className="text-slate-600">
              {project.isPending ? "Loading…" : (project.data?.name ?? "")}
            </span>
            <button
              type="button"
              onClick={signOut}
              disabled={signingOut}
              className="rounded-md border border-slate-300 px-3 py-1.5 font-medium hover:bg-slate-50 disabled:opacity-60 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
            >
              Sign out
            </button>
          </div>
        </div>
      </header>
      <div className="mx-auto max-w-5xl px-4 py-6">
        {project.isError ? (
          <div role="alert" className="rounded-md bg-red-50 p-4 text-sm text-red-800">
            Could not load your project.{" "}
            <button type="button" onClick={() => void project.refetch()} className="font-medium underline">
              Retry
            </button>
          </div>
        ) : project.ensured ? (
          // Children (and their queries) mount only after the project exists.
          children
        ) : (
          <p role="status" className="text-sm text-slate-600">Setting up your project…</p>
        )}
      </div>
      {project.apiKey && <ApiKeyOnceDialog apiKey={project.apiKey} onClose={project.dismissKey} />}
    </div>
  );
}
