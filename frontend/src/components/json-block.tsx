import { focusRing } from "@/components/ui";

/** Pretty-printed JSON as text (React escapes it; never HTML). Focusable so long payloads scroll by keyboard. */
export function JsonBlock({ value, label }: { value: unknown; label: string }) {
  return (
    <pre
      tabIndex={0}
      aria-label={label}
      className={`max-h-96 overflow-auto rounded-md bg-slate-900 p-4 font-mono text-xs text-slate-100 ${focusRing}`}
    >
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}
