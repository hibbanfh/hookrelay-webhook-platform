"use client";

import { btn } from "@/components/ui";
import { asApiError, displayMessage } from "@/lib/api/errors";
import type { DeliveryStatus } from "@/lib/api/types";
import { useReplayDelivery } from "@/lib/queries/deliveries";

interface Props {
  messageId: string;
  deliveryId: string;
  status: DeliveryStatus;
}

export function ReplayButton({ messageId, deliveryId, status }: Props) {
  const replay = useReplayDelivery(messageId);
  const pending = status === "pending";
  return (
    <div className="flex flex-col items-start gap-1">
      <button
        type="button"
        className={btn}
        disabled={pending || replay.isPending}
        aria-describedby={pending ? `replay-help-${deliveryId}` : undefined}
        onClick={() => replay.mutate(deliveryId)}
      >
        Replay
      </button>
      {pending && (
        <p id={`replay-help-${deliveryId}`} className="text-xs text-slate-600">
          Replay is available once the delivery finishes
        </p>
      )}
      {replay.isError && (
        <p role="alert" className="text-xs text-red-700">
          {displayMessage(asApiError(replay.error))}
        </p>
      )}
    </div>
  );
}
