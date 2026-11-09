import Link from "next/link";

export default function Home() {
  return (
    <main className="mx-auto flex min-h-screen max-w-2xl flex-col justify-center gap-4 px-6">
      <h1 className="text-4xl font-semibold tracking-tight">Hookrelay</h1>
      <p className="text-slate-600">
        Publish webhooks with one API call. Hookrelay signs, delivers, retries and logs them.
      </p>
      <nav aria-label="Account" className="flex gap-3 text-sm">
        <Link href="/sign-up" className="rounded-md bg-indigo-600 px-3 py-2 font-medium text-white hover:bg-indigo-700">Create account</Link>
        <Link href="/sign-in" className="rounded-md border border-slate-300 px-3 py-2 font-medium hover:bg-slate-50">Sign in</Link>
      </nav>
    </main>
  );
}
