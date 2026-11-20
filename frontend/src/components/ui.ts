// Shared Tailwind class strings, so focus rings and sizes stay consistent.
export const focusRing = "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600";
const focus = focusRing;

export const btn = `rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60 ${focus}`;
export const btnPrimary = `rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-60 ${focus}`;
export const input = `block w-full rounded-md border border-slate-300 px-3 py-2 text-sm ${focus}`;
export const link = `text-indigo-700 underline hover:text-indigo-900 ${focus}`;
export const th = "px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-slate-600";
export const td = "px-3 py-2 align-top text-sm";
