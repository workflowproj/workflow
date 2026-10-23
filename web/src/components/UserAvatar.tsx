import { Bot, UserRound } from "lucide-react";
import { cn } from "@/lib/cn";
import { initials } from "@/lib/format";

type U = { displayName: string; avatarUrl?: string; kind?: string };

const palette = ["#2563eb", "#7c3aed", "#db2777", "#ea580c", "#0d9488", "#4f46e5", "#0891b2", "#65a30d"];

function colorFor(name: string) {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) | 0;
  return palette[Math.abs(h) % palette.length];
}

/** UserAvatar shows a user's picture or initials; `undefined` renders the "unassigned" glyph. */
export function UserAvatar({ user, size = 24, className }: { user?: U | null; size?: number; className?: string }) {
  const style = { width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.4)) };
  if (!user) {
    return (
      <span title="Unassigned" style={style} className={cn("inline-flex shrink-0 items-center justify-center rounded-full border border-dashed border-line-strong text-faint", className)}>
        <UserRound className="size-[60%]" />
      </span>
    );
  }
  if (user.avatarUrl) {
    return <img src={user.avatarUrl} alt="" title={user.displayName} referrerPolicy="no-referrer" style={style} className={cn("shrink-0 rounded-full object-cover", className)} />;
  }
  return (
    <span title={user.displayName} style={{ ...style, background: colorFor(user.displayName) }} className={cn("inline-flex shrink-0 items-center justify-center rounded-full font-semibold text-white", className)}>
      {user.kind === "service" || user.kind === "automation" ? <Bot className="size-[60%]" /> : initials(user.displayName)}
    </span>
  );
}
