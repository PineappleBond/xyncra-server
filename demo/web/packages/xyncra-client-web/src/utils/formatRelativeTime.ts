/**
 * Format a date string as a human-readable relative time in Chinese.
 * Matches the Vue version's formatRelativeTime output.
 *
 * - "刚刚"           (< 1 min)
 * - "N分钟前"        (< 60 min)
 * - "N小时前"        (< 24 h)
 * - "N天前"          (< 7 d)
 * - "MM-DD HH:mm"   (older)
 */
export function formatRelativeTime(dateStr: string | undefined): string {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  if (Number.isNaN(date.getTime())) return '';

  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMinutes = Math.floor(diffMs / 60_000);

  if (diffMinutes < 1) return '刚刚';
  if (diffMinutes < 60) return `${diffMinutes}分钟前`;

  const diffHours = Math.floor(diffMinutes / 60);
  if (diffHours < 24) return `${diffHours}小时前`;

  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 7) return `${diffDays}天前`;

  // Older than 7 days: show MM-DD HH:mm
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  const hh = String(date.getHours()).padStart(2, '0');
  const min = String(date.getMinutes()).padStart(2, '0');
  return `${mm}-${dd} ${hh}:${min}`;
}
