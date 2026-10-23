import { useRef } from "react";
import { cn } from "@/lib/cn";

/**
 * YamlEditor is a monospace text area with line numbers. `errorLine` (1-based) is highlighted.
 * Tab keeps moving focus (indent with spaces) so the editor never traps the keyboard.
 */
export function YamlEditor({
  value,
  onChange,
  readOnly,
  errorLine,
  className,
  label = "Workflow YAML",
}: {
  value: string;
  onChange?: (v: string) => void;
  readOnly?: boolean;
  errorLine?: number;
  className?: string;
  label?: string;
}) {
  const gutter = useRef<HTMLDivElement>(null);
  const lines = value.split("\n").length;
  return (
    <div className={cn("relative flex overflow-hidden rounded-md border border-line bg-surface font-mono text-[12.5px] leading-[1.6]", className)}>
      <div ref={gutter} aria-hidden className="w-11 shrink-0 overflow-hidden border-r border-line bg-surface-2 py-3 text-right text-faint select-none">
        {Array.from({ length: lines }, (_, i) => (
          <div key={i} className={cn("pr-2", errorLine === i + 1 && "bg-danger-soft font-semibold text-danger")}>
            {i + 1}
          </div>
        ))}
      </div>
      <textarea
        aria-label={label}
        value={value}
        readOnly={readOnly}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        wrap="off"
        onScroll={(e) => {
          if (gutter.current) gutter.current.scrollTop = e.currentTarget.scrollTop;
        }}
        onChange={(e) => onChange?.(e.target.value)}
        className="min-w-0 flex-1 resize-none overflow-auto bg-transparent px-3 py-3 whitespace-pre text-ink outline-none"
      />
    </div>
  );
}
