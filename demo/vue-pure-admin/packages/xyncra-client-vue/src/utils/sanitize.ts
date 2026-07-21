import DOMPurify from 'dompurify'

/**
 * Sanitize HTML content to prevent XSS attacks.
 *
 * This function uses DOMPurify to clean HTML content before rendering.
 * It allows common HTML tags and attributes needed for Markdown rendering
 * while stripping potentially dangerous content like <script> tags,
 * event handlers, and javascript: URLs.
 */
export function sanitizeHtml(html: string): string {
  return DOMPurify.sanitize(html, {
    // Allow common HTML tags used in Markdown rendering
    ALLOWED_TAGS: [
      'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
      'p', 'br', 'hr',
      'ul', 'ol', 'li',
      'blockquote', 'pre', 'code',
      'em', 'strong', 'del', 's', 'u',
      'a', 'img',
      'table', 'thead', 'tbody', 'tr', 'th', 'td',
      'span', 'div',
      'input', // For task lists
    ],
    // Allow common attributes
    ALLOWED_ATTR: [
      'href', 'target', 'rel', // For links
      'src', 'alt', 'width', 'height', // For images
      'class', // For syntax highlighting
      'type', 'checked', 'disabled', // For task list checkboxes
      'start', // For ordered lists
    ],
    // Allow href attributes to be preserved
    ALLOW_DATA_ATTR: false,
  })
}
