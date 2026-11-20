/** A machine-readable `<time>` with a locale-formatted label. Pure: no timers. */
export function RelativeTime({ iso }: { iso: string }) {
  const date = new Date(iso);
  const label = Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
  return <time dateTime={iso}>{label}</time>;
}
