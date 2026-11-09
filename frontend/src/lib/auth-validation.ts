export interface FieldError {
  field: "name" | "email" | "password";
  message: string;
}

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function validateEmail(email: string): string | null {
  return email.length <= 254 && EMAIL.test(email) ? null : "Enter a valid email address";
}

export function validatePassword(password: string): string | null {
  if (password.length < 8) return "Password must be at least 8 characters";
  return password.length <= 128 ? null : "Password must be at most 128 characters";
}

export function validateName(name: string): string | null {
  if (name.length === 0) return "Name is required";
  return name.length <= 100 ? null : "Name must be at most 100 characters";
}

export function validateSignUp(input: { name: string; email: string; password: string }): FieldError | null {
  const name = validateName(input.name);
  if (name) return { field: "name", message: name };
  const email = validateEmail(input.email);
  if (email) return { field: "email", message: email };
  const password = validatePassword(input.password);
  return password ? { field: "password", message: password } : null;
}

export function validateSignIn(input: { email: string; password: string }): FieldError | null {
  const email = validateEmail(input.email);
  if (email) return { field: "email", message: email };
  if (input.password.length === 0) return { field: "password", message: "Password is required" };
  return null;
}
