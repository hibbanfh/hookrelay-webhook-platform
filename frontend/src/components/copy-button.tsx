"use client";

import { useState } from "react";
import { btn } from "@/components/ui";

interface Props {
  /** The text to copy. It is only read on click; nothing is stored. */
  value: string;
  label?: string;
}

/** Copies a string to the clipboard and announces the result in a status region. */
export function CopyButton({ value, label = "Copy" }: Props) {
  const [copied, setCopied] = useState<"idle" | "copied" | "failed">("idle");

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied("copied");
    } catch {
      setCopied("failed");
    }
  }

  return (
    <>
      <button type="button" onClick={copy} className={btn}>
        {label}
      </button>
      <span role="status" className="text-sm text-slate-600">
        {copied === "copied" && "Copied to clipboard"}
        {copied === "failed" && "Copy failed. Select the text and copy it manually."}
      </span>
    </>
  );
}
