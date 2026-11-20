import type { ReactNode } from "react";

interface Props {
  title: string;
  children?: ReactNode;
  /** The next step (for example a curl example). */
  action?: ReactNode;
}

export function EmptyState({ title, children, action }: Props) {
  return (
    <div className="rounded-lg border border-dashed border-slate-300 bg-white p-6">
      <h2 className="text-base font-semibold">{title}</h2>
      {children && <div className="mt-1 text-sm text-slate-600">{children}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
