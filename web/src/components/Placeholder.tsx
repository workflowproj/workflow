import { Construction } from "lucide-react";
import { EmptyState } from "./ui";

/** Placeholder marks a page that is not built yet. */
export function Placeholder({ name }: { name: string }) {
  return <EmptyState icon={<Construction className="size-8" />} title={name} body="This page is coming soon." />;
}
