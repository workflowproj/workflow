import { useMemo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useQuery } from "@tanstack/react-query";
import { projectsQuery } from "@/api/queries";
import { cn } from "@/lib/cn";
import { linkIssueKeys, projectKeySet } from "@/lib/issue-keys";

/**
 * Markdown renders user content safely (raw HTML is not rendered). Keys of issues in projects
 * the viewer can browse link to the issues, except with `issueLinks={false}` (the customer
 * portal, whose people cannot open issues).
 */
export function Markdown({ children, className, issueLinks = true }: { children: string; className?: string; issueLinks?: boolean }) {
  const { data: projects } = useQuery({ ...projectsQuery, enabled: issueLinks });
  const keys = useMemo(() => projectKeySet(projects), [projects]);
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
        {issueLinks ? linkIssueKeys(children, keys) : children}
      </ReactMarkdown>
    </div>
  );
}
