import type { ReactNode } from "react";
import { Link } from "@carbon/react";

// Normalizes a string by converting to lowercase and replacing spaces with hyphens
export const normalizeString = (str: string): string => {
  return str.toLowerCase().replace(/\s+/g, "-");
};

// Renders text with double line breaks as separate paragraphs
export const renderParagraphs = (text: string): ReactNode => {
  return text.split("\n\n").map((paragraph, index, array) => (
    <span key={index}>
      {paragraph}
      {index < array.length - 1 && (
        <>
          <br />
          <br />
        </>
      )}
    </span>
  ));
};

// Parses Markdown links in text and converts them to clickable Carbon Link components
// Matches pattern: [text](url) and converts to <Link href="url">text</Link>
export const parseMarkdownLinks = (text: string): ReactNode => {
  // Match Markdown link pattern: [text](url)
  const linkRegex = /\[([^\]]+)\]\(([^)]+)\)/g;
  const parts: ReactNode[] = [];
  let lastIndex = 0;
  let match;

  while ((match = linkRegex.exec(text)) !== null) {
    // Add text before the link
    if (match.index > lastIndex) {
      parts.push(text.substring(lastIndex, match.index));
    }

    // Add the link
    const linkText = match[1];
    const url = match[2];
    parts.push(
      <Link
        key={match.index}
        href={url}
        target="_blank"
        rel="noopener noreferrer"
      >
        {linkText}
      </Link>,
    );

    lastIndex = match.index + match[0].length;
  }

  // Add remaining text after the last link
  if (lastIndex < text.length) {
    parts.push(text.substring(lastIndex));
  }

  return parts.length > 0 ? parts : text;
};
