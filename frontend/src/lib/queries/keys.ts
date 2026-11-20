/** The shell's "ensure project" query (POST /v1/me/project). */
export const PROJECT_KEY = ["me", "project"] as const;

/** One factory for every query key, so invalidation is never a typo. */
export const keys = {
  project: () => ["project"] as const,
  endpointsList: () => ["endpoints", "list"] as const,
  messagesList: () => ["messages", "list"] as const,
  message: (id: string) => ["messages", id] as const,
  attempts: (deliveryId: string) => ["deliveries", deliveryId, "attempts"] as const,
};
