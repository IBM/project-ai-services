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
