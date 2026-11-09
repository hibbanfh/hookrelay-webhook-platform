import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE } from "@/lib/session-cookie";

const AUTH_PAGES = new Set(["/sign-in", "/sign-up"]);

/** Presence check only. Expired or signed-out sessions are caught by the 401 -> /sign-in redirect in the client. */
export function middleware(req: NextRequest) {
  const { pathname, search } = req.nextUrl;
  const signedIn = req.cookies.has(SESSION_COOKIE);

  if (AUTH_PAGES.has(pathname)) {
    return signedIn ? redirect(req, "/endpoints") : NextResponse.next();
  }
  if (signedIn) return NextResponse.next();

  const url = req.nextUrl.clone();
  url.pathname = "/sign-in";
  url.search = "";
  url.searchParams.set("next", pathname + search);
  return NextResponse.redirect(url);
}

function redirect(req: NextRequest, path: string) {
  const url = req.nextUrl.clone();
  url.pathname = path;
  url.search = "";
  return NextResponse.redirect(url);
}

export const config = {
  // Everything except the BFF routes, Next internals and files with an extension. "/" stays public (landing).
  matcher: ["/((?!api/|auth/|_next/|favicon.ico|.*\\..*).+)"],
};
