import { XMarkdown } from '@ant-design/x-markdown';

interface MarkdownRendererProps {
  content: string;
}

export function MarkdownRenderer({ content }: MarkdownRendererProps) {
  if (!content) {
    return null;
  }

  return (
    <XMarkdown
      content={content}
      openLinksInNewTab
    />
  );
}
