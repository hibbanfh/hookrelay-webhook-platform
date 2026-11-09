"use client";

import { AuthForm } from "@/components/auth-form";

export default function SignUpPage() {
  // Full navigation so the new cookie is picked up by middleware and the shell starts clean.
  return <AuthForm mode="sign-up" onSuccess={() => window.location.assign("/endpoints")} />;
}
