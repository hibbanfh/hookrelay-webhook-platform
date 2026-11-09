"use client";

import Link from "next/link";
import { useState, type FormEvent } from "react";
import {
  validateEmail,
  validateName,
  validatePassword,
  type FieldError,
} from "@/lib/auth-validation";

type Mode = "sign-in" | "sign-up";
type Field = FieldError["field"];
type Errors = Partial<Record<Field, string>>;

interface Props {
  mode: Mode;
  /** Called after the server accepted the credentials (cookie already set). */
  onSuccess: () => void;
}

const COPY = {
  "sign-in": { title: "Sign in", submit: "Sign in", alt: "No account?", altLink: "/sign-up", altText: "Create one" },
  "sign-up": { title: "Create your account", submit: "Create account", alt: "Already registered?", altLink: "/sign-in", altText: "Sign in" },
} as const;

function validate(mode: Mode, v: Record<Field, string>): Errors {
  const errors: Errors = {};
  if (mode === "sign-up") {
    const name = validateName(v.name.trim());
    if (name) errors.name = name;
  }
  const email = validateEmail(v.email.trim());
  if (email) errors.email = email;
  const password = mode === "sign-up" ? validatePassword(v.password) : v.password ? null : "Password is required";
  if (password) errors.password = password;
  return errors;
}

export function AuthForm({ mode, onSuccess }: Props) {
  const copy = COPY[mode];
  const [errors, setErrors] = useState<Errors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const values = {
      name: String(data.get("name") ?? ""),
      email: String(data.get("email") ?? ""),
      password: String(data.get("password") ?? ""),
    };
    const found = validate(mode, values);
    setErrors(found);
    setFormError(null);
    if (Object.keys(found).length > 0) return;

    setPending(true);
    try {
      const res = await fetch(`/auth/${mode}`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(mode === "sign-up" ? values : { email: values.email, password: values.password }),
      });
      if (res.ok) return onSuccess();
      await showServerError(res);
    } catch {
      setFormError("Could not reach the server. Check your connection and try again.");
    } finally {
      setPending(false);
    }
  }

  async function showServerError(res: Response) {
    const body: unknown = await res.json().catch(() => null);
    const err = (body as { error?: { message?: string; field?: Field } } | null)?.error;
    if (err?.field && err.message) return setErrors({ [err.field]: err.message });
    if (res.status === 503) return setFormError("Authentication is temporarily unavailable. Try again in a moment.");
    setFormError(err?.message ?? "Something went wrong. Try again.");
  }

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-sm flex-col justify-center px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-tight">{copy.title}</h1>
      <form onSubmit={submit} noValidate className="mt-6 flex flex-col gap-4">
        {formError && (
          <p role="alert" className="rounded-md bg-red-50 p-3 text-sm text-red-800">
            {formError}
          </p>
        )}
        {mode === "sign-up" && (
          <TextField id="name" label="Name" autoComplete="name" error={errors.name} />
        )}
        <TextField id="email" label="Email" type="email" autoComplete="email" error={errors.email} />
        <TextField
          id="password"
          label="Password"
          type="password"
          autoComplete={mode === "sign-up" ? "new-password" : "current-password"}
          hint={mode === "sign-up" ? "At least 8 characters" : undefined}
          error={errors.password}
        />
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-60 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
        >
          {pending ? "Please wait…" : copy.submit}
        </button>
      </form>
      <p className="mt-4 text-sm text-slate-600">
        {copy.alt}{" "}
        <Link href={copy.altLink} className="font-medium text-indigo-700 underline">
          {copy.altText}
        </Link>
      </p>
    </main>
  );
}

function TextField(props: {
  id: Field;
  label: string;
  type?: string;
  autoComplete: string;
  hint?: string;
  error?: string;
}) {
  const { id, label, type = "text", autoComplete, hint, error } = props;
  const describedBy = [error ? `${id}-error` : null, hint && !error ? `${id}-hint` : null].filter(Boolean).join(" ");
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      <input
        id={id}
        name={id}
        type={type}
        autoComplete={autoComplete}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy || undefined}
        className="rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-indigo-600 aria-[invalid=true]:border-red-600"
      />
      {hint && !error && (
        <p id={`${id}-hint`} className="text-xs text-slate-600">
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className="text-sm text-red-700">
          {error}
        </p>
      )}
    </div>
  );
}
