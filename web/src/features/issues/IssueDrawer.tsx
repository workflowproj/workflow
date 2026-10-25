import { Drawer } from "@/components/ui";
import { IssueView } from "./IssueView";

/** IssueDrawer shows IssueView in a side panel; pages keep the open key in `?issue=KEY`. */
export function IssueDrawer({ issueKey, onClose, onNavigate }: { issueKey?: string; onClose: () => void; onNavigate: (key: string) => void }) {
  return (
    <Drawer open={!!issueKey} onClose={onClose} label={issueKey ? `Issue ${issueKey}` : "Issue"}>
      {issueKey && <IssueView issueKey={issueKey} variant="drawer" onClose={onClose} onNavigate={onNavigate} />}
    </Drawer>
  );
}
