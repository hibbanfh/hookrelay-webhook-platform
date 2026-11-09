"use client";

import { AuthForm } from "@/components/auth-form";
import { safeNext } from "@/lib/safe-next";

export default function SignInPage() {
  const go = () => window.location.assign(safeNext(new URLSearchParams(window.location.search).get("next")));
  return <AuthForm mode="sign-in" onSuccess={go} />;
}
