import { validateSignIn } from "@/lib/auth-validation";
import { finishAuth, guardOrigin, invalidField, readJsonFields } from "@/lib/server/auth-route";
import { errorResponse } from "@/lib/server/http";
import { signIn } from "@/lib/server/neon-auth";

export async function POST(req: Request) {
  const blocked = guardOrigin(req);
  if (blocked) return blocked;

  const fields = await readJsonFields(req);
  const input = { email: (fields.email ?? "").trim(), password: fields.password ?? "" };
  const invalid = validateSignIn(input);
  if (invalid) return invalidField(invalid);

  // email_taken cannot happen on sign-in; treat it as a generic failure rather than leaking anything.
  return finishAuth(await signIn(input), () =>
    errorResponse(401, "invalid_credentials", "Invalid email or password"),
  );
}
