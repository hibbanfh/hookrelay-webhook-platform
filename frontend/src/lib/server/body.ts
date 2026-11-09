export const MAX_BODY_BYTES = 256 * 1024;

/** Reads a request body up to 256 KB (same limit as the Go API); returns null when it is larger. */
export async function readBodyCapped(req: Request, max = MAX_BODY_BYTES): Promise<ArrayBuffer | null> {
  const declared = Number(req.headers.get("content-length") ?? "0");
  if (declared > max) return null;
  const buf = await req.arrayBuffer();
  return buf.byteLength > max ? null : buf;
}
