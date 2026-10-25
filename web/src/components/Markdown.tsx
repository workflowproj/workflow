import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/cn";

/**
 * Markdown renders user content safely (raw HTML is not rendered). Issue keys link to issues,
 * except with `issueLinks={false}` (the customer portal, whose people cannot open issues).
 */
export function Markdown({ children, className, issueLinks = true }: { children: string; className?: string; issueLinks?: boolean }) {
  return (
    <div className={cn("prose-wfm text-sm", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children }) => (
            <a href={href} target={href?.startsWith("/") ? undefined : "_blank"} rel="noreferrer noopener">
              {children}
            </a>
          ),
        }}
      >
        {issueLinks ? linkIssueKeys(children) : children}
      </ReactMarkdown>
    </div>
  );
}

// Turns bare issue keys (ABC-123) into links, leaving code spans and existing links alone.
function linkIssueKeys(md: string): string {
  return md
    .split(/(```[\s\S]*?```|`[^`]*`|\[[^\]]*\]\([^)]*\))/g)
    .map((part, i) => (i % 2 === 1 ? part : part.replace(/(^|[^\w/-])([A-Z][A-Z0-9_]{1,9}-\d+)\b/g, "$1[$2](/browse/$2)")))
    .join("");
}
