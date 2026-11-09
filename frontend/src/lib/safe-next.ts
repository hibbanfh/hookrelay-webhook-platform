const BASE = "http://dashboard.invalid";

/** Only same-site absolute paths are valid post-login targets (blocks open redirects). */
export function safeNext(next: string | null | undefined, fallback = "/endpoints"): string {
  if (!next || !next.startsWith("/") || next.startsWith("//")) return fallback;
  // Control characters (tab/CR/LF are stripped by URL parsers, turning "/\t/x" into "//x") and backslashes.
  if (/[\u0000-\u001f\u007f\\]/.test(next)) return fallback;
  let url: URL;
  try {
    url = new URL(next, BASE);
  } catch {
    return fallback;
  }
  if (url.origin !== BASE) return fallback;
  const path = url.pathname + url.search + url.hash;
  if (path.startsWith("/sign-in") || path.startsWith("/sign-up") || path.startsWith("/auth/")) return fallback;
  return next;
}
