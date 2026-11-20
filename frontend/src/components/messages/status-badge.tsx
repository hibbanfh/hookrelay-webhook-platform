import type { DeliveryStatus } from "@/lib/api/types";

const STYLE: Record<DeliveryStatus, string> = {
  pending: "bg-amber-100 text-amber-900",
  succeeded: "bg-green-100 text-green-900",
  failed: "bg-red-100 text-red-900",
};

/** The label is text, so meaning never depends on colour alone. */
export function StatusBadge({ status }: { status: DeliveryStatus }) {
  return (
    <span data-testid="delivery-status" className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${STYLE[status]}`}>
      {status}
    </span>
  );
}
