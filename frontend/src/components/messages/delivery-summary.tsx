import type { DeliveryCounts } from "@/lib/api/types";

export const NO_DELIVERIES_TEXT = "No endpoints were enabled when this message was published";

export function DeliverySummary({ counts }: { counts: DeliveryCounts }) {
  if (counts.succeeded === 0 && counts.failed === 0 && counts.pending === 0) {
    return <span className="text-slate-600">{NO_DELIVERIES_TEXT}</span>;
  }
  return (
    <span>
      <span className="text-green-800">{counts.succeeded} succeeded</span>
      {" / "}
      <span className="text-red-800">{counts.failed} failed</span>
      {" / "}
      <span className="text-amber-800">{counts.pending} pending</span>
    </span>
  );
}
