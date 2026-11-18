// Calculates and formats the uptime duration from a creation timestamp
export function calculateUptime(createdAt: string): string {
  const created = new Date(createdAt);
  const now = new Date();
  const diffMs = now.getTime() - created.getTime();

  if (Number.isNaN(diffMs)) {
    return "Unknown";
  }

  const totalSeconds = Math.floor(diffMs / 1000);
  const totalMinutes = Math.floor(totalSeconds / 60);
  const totalHours = Math.floor(totalMinutes / 60);
  const totalDays = Math.floor(totalHours / 24);

  if (totalDays > 0) {
    return totalDays === 1 ? "1 day" : `${totalDays} days`;
  } else if (totalHours > 0) {
    return totalHours === 1 ? "1 hour" : `${totalHours} hours`;
  } else if (totalMinutes > 0) {
    return totalMinutes === 1 ? "1 minute" : `${totalMinutes} minutes`;
  } else {
    return totalSeconds === 1
      ? "1 second"
      : totalSeconds > 0
        ? `${totalSeconds} seconds`
        : "Just now";
  }
}

// Formats an API timestamp as "02 Jan 2026, 12:22" (24-hour) in the user's local timezone.
export function formatDateTime(timestamp?: string | null): string {
  if (!timestamp) {
    return "—";
  }

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }

  const parts = new Intl.DateTimeFormat("en-GB", {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(date);

  const get = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((p) => p.type === type)?.value ?? "";

  return `${get("day")} ${get("month")} ${get("year")}, ${get("hour")}:${get("minute")}`;
}
