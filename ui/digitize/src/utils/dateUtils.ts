/**
 * Calculate the duration between a start time and end time (or now if not completed)
 * @param startTime - ISO 8601 date string
 * @param endTime - ISO 8601 date string (optional, defaults to current time)
 * @returns Formatted duration string (e.g., "2d 3h 45m" or "5m 30s")
 */
export const calculateDuration = (startTime: string | undefined, endTime?: string | undefined): string => {
  if (!startTime) return 'N/A';
  
  const start = new Date(startTime);
  const end = endTime ? new Date(endTime) : new Date();
  const diffMs = end.getTime() - start.getTime();
  
  // Handle negative durations (shouldn't happen but just in case)
  if (diffMs < 0) return 'N/A';
  
  const totalSeconds = Math.floor(diffMs / 1000);
  const totalMinutes = Math.floor(totalSeconds / 60);
  const totalHours = Math.floor(totalMinutes / 60);
  const days = Math.floor(totalHours / 24);
  
  const hours = totalHours % 24;
  const minutes = totalMinutes % 60;
  const seconds = totalSeconds % 60;
  
  const parts = [];
  
  if (days > 0) {
    parts.push(`${days}d`);
  }
  if (hours > 0 || days > 0) {
    parts.push(`${hours}h`);
  }
  if (minutes > 0 || hours > 0 || days > 0) {
    parts.push(`${minutes}m`);
  }
  if (parts.length === 0) {
    parts.push(`${seconds}s`);
  }
  
  return parts.join(' ');
};

// Made with Bob