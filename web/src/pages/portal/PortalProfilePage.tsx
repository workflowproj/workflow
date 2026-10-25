import { Lock } from "lucide-react";
import { useMe } from "@/api/queries";
import { PageSpinner } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { SettingsSection } from "@/features/settings/common";
import { EmailNotificationsSection } from "@/features/settings/email/EmailNotificationsSection";
import { SignInSecurity } from "@/features/auth/SignInSecurity";

/** PortalProfilePage: the account, how to sign in, and which request emails to get. */
export function PortalProfilePage() {
  const { data: me } = useMe();
  if (!me) return <PageSpinner />;
  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Profile</h1>
        <p className="mt-1 text-sm text-muted">Your account, how you sign in and the emails we send you about your requests.</p>
      </div>
      <SettingsSection title="Account">
        <div className="flex flex-wrap items-center gap-4 p-5">
          <UserAvatar user={me} size={56} />
          <dl className="grid min-w-0 flex-1 grid-cols-[6rem_1fr] gap-x-4 gap-y-1.5 text-sm">
            <dt className="text-muted">Name</dt>
            <dd className="font-medium">{me.displayName}</dd>
            <dt className="text-muted">Email</dt>
            <dd className="break-all">{me.email}</dd>
          </dl>
        </div>
        <p className="flex items-start gap-2 border-t border-line px-5 py-3 text-[13px] text-muted">
          <Lock aria-hidden className="mt-0.5 size-3.5 shrink-0" />
          Ask the help desk team to change your name or email address.
        </p>
      </SettingsSection>
      <SignInSecurity />
      <EmailNotificationsSection />
    </div>
  );
}
