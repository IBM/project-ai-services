import type { ReactNode } from "react";

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
