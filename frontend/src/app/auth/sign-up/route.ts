import { validateSignUp } from "@/lib/auth-validation";
import { finishAuth, guardOrigin, invalidField, readJsonFields } from "@/lib/server/auth-route";
import { errorResponse } from "@/lib/server/http";
import { signUp } from "@/lib/server/neon-auth";

export async function POST(req: Request) {
  const blocked = guardOrigin(req);
  if (blocked) return blocked;

  const fields = await readJsonFields(req);
  const input = { name: (fields.name ?? "").trim(), email: (fields.email ?? "").trim(), password: fields.password ?? "" };
  const invalid = validateSignUp(input);
  if (invalid) return invalidField(invalid);

  return finishAuth(await signUp(input), () =>
    errorResponse(409, "email_taken", "An account with this email already exists", "email"),
  );
}
