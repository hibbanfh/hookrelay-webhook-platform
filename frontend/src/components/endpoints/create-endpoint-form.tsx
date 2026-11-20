"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { btn, btnPrimary, input } from "@/components/ui";
import { asApiError, displayMessage, fieldErrors } from "@/lib/api/errors";
import { useCreateEndpoint } from "@/lib/queries/endpoints";
import { useProject } from "@/lib/queries/project";

const FIELDS = ["url", "description"] as const;
type Field = (typeof FIELDS)[number];
type Errors = Partial<Record<Field, string>>;

export function CreateEndpointForm() {
  const project = useProject();
  const create = useCreateEndpoint();
  const [url, setUrl] = useState("");
  const [description, setDescription] = useState("");
  const [errors, setErrors] = useState<Errors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const urlRef = useRef<HTMLInputElement>(null);
  const descriptionRef = useRef<HTMLInputElement>(null);

  // Move focus to the first invalid input after an error render.
  useEffect(() => {
    if (errors.url) urlRef.current?.focus();
    else if (errors.description) descriptionRef.current?.focus();
  }, [errors]);

  const demoUrl = project.data?.demo_receiver_url ?? null;

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setFormError(null);
    if (url.trim() === "") {
      setErrors({ url: "URL is required" });
      return;
    }
    setErrors({});
    try {
      await create.mutateAsync({ url: url.trim(), description: description.trim() });
      setUrl("");
      setDescription("");
    } catch (err) {
      const apiError = asApiError(err);
      const found = fieldErrors(apiError, FIELDS);
      setErrors(found);
      if (Object.keys(found).length === 0) setFormError(displayMessage(apiError));
    }
  }

  return (
    <form onSubmit={submit} noValidate aria-labelledby="create-endpoint-title" className="rounded-lg border border-slate-200 bg-white p-4">
      <h2 id="create-endpoint-title" className="text-base font-semibold">
        Add an endpoint
      </h2>
      {formError && (
        <p role="alert" className="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-800">
          {formError}
        </p>
      )}
      <div className="mt-3 grid gap-4 sm:grid-cols-2">
        <div>
          <label htmlFor="endpoint-url" className="block text-sm font-medium">
            URL
          </label>
          <input
            id="endpoint-url"
            ref={urlRef}
            type="url"
            inputMode="url"
            autoComplete="off"
            placeholder="https://example.com/webhooks"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            aria-invalid={errors.url ? true : undefined}
            aria-describedby={errors.url ? "endpoint-url-error" : undefined}
            className={`mt-1 ${input}`}
          />
          {errors.url && (
            <p id="endpoint-url-error" className="mt-1 text-sm text-red-700">
              {errors.url}
            </p>
          )}
          {demoUrl && (
            <button type="button" className={`mt-2 ${btn}`} onClick={() => setUrl(demoUrl)}>
              Use demo receiver
            </button>
          )}
        </div>
        <div>
          <label htmlFor="endpoint-description" className="block text-sm font-medium">
            Description <span className="font-normal text-slate-600">(optional)</span>
          </label>
          <input
            id="endpoint-description"
            ref={descriptionRef}
            type="text"
            autoComplete="off"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            aria-invalid={errors.description ? true : undefined}
            aria-describedby={errors.description ? "endpoint-description-error" : undefined}
            className={`mt-1 ${input}`}
          />
          {errors.description && (
            <p id="endpoint-description-error" className="mt-1 text-sm text-red-700">
              {errors.description}
            </p>
          )}
        </div>
      </div>
      <div className="mt-4">
        <button type="submit" disabled={create.isPending} className={btnPrimary}>
          {create.isPending ? "Creating…" : "Create endpoint"}
        </button>
      </div>
    </form>
  );
}
