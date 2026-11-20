// Liveness probe for Fly and Docker. Public (middleware skips /api/*) and touches no env or upstream.
export const dynamic = "force-dynamic";

export function GET() {
  return Response.json({ status: "ok" });
}
