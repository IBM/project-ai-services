// Calculates and formats the uptime duration from a creation timestamp
export function calculateUptime(createdAt: string): string {
  const created = new Date(createdAt);
  const now = new Date();
  const diffMs = now.getTime() - created.getTime();

  const totalSeconds = Math.floor(diffMs / 1000);
  const totalMinutes = Math.floor(totalSeconds / 60);
  const totalHours = Math.floor(totalMinutes / 60);
  const totalDays = Math.floor(totalHours / 24);

  const minutes = totalMinutes % 60;
  const hours = totalHours % 24;

  if (totalDays > 0) {
    const days = hours > 0 ? totalDays + 1 : totalDays;
    return days === 1 ? "1 day" : `${days} days`;
  } else if (totalHours > 0) {
    const hrs = minutes > 0 ? totalHours + 1 : totalHours;
    return hrs === 1 ? "1 hour" : `${hrs} hours`;
  } else if (totalMinutes > 0) {
    const mins = totalSeconds % 60 > 0 ? totalMinutes + 1 : totalMinutes;
    return mins === 1 ? "1 minute" : `${mins} minutes`;
  } else {
    return totalSeconds === 1
      ? "1 second"
      : totalSeconds > 0
        ? `${totalSeconds} seconds`
        : "Just now";
  }
}
