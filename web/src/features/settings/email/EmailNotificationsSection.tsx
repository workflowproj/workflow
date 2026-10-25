import { useEffect, useMemo, useRef, useState } from "react";
import { useIsMutating, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, MailX } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Button, Field, PageSpinner, Select, table, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, SettingsSection, Switch, When } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { applyPatch, browserTimeZone, deliveries, formatHour, mergePatch, nextDigestLabel, preferencePatch, timeZoneOptions } from "./model";

type Patch = Schemas["EmailNotificationSettingsPatch"];

const key = ["email-notifications"];
const mutationKey = ["email-notifications", "save"];

/** EmailNotificationsSection lets people choose, per kind of notification, whether it is emailed immediately, in a daily digest or not at all. */
export function EmailNotificationsSection() {
  const qc = useQueryClient();
  const toast = useToast();
  const ref = useRef<HTMLDivElement>(null);
  const q = useQuery({ queryKey: key, queryFn: () => unwrap(api.GET("/me/email-notifications")) });
  const mutation = useMutation({
    mutationKey,
    mutationFn: (body: Patch) => unwrap(api.PATCH("/me/email-notifications", { body })),
    // One at a time, so responses cannot arrive out of order.
    scope: { id: "email-notifications" },
    onSuccess: (data) => qc.setQueryData(key, data),
    onError: (err) => {
      toast.error(err);
      qc.invalidateQueries({ queryKey: key });
    },
  });
  // Changes show at once (component state updates in the event, so controlled radios never
  // flick back) and give way to the saved settings once every save has finished.
  const [unsaved, setUnsaved] = useState<Patch>({});
  const saving = useIsMutating({ mutationKey });
  useEffect(() => {
    if (saving === 0) setUnsaved({});
  }, [saving]);
  const save = (body: Patch) => {
    setUnsaved((u) => mergePatch(u, body));
    mutation.mutate(body);
  };
  const loaded = !!q.data;
  useEffect(() => {
    // Emails link here ("Manage email notifications").
    if (loaded && window.location.hash === "#email-notifications") ref.current?.scrollIntoView({ block: "start" });
  }, [loaded]);
  const s = useMemo(() => q.data && applyPatch(q.data, unsaved), [q.data, unsaved]);
  const zones = useMemo(() => timeZoneOptions(s?.timeZone ?? "UTC"), [s?.timeZone]);
  const here = browserTimeZone();
  return (
    <div id="email-notifications" ref={ref} className="scroll-mt-6">
      <SettingsSection
        title="Email notifications"
        description={s ? `Sent to ${s.address}. In-app notifications are not affected.` : "How notifications reach your inbox."}
        actions={
          s && (
            <>
              <SaveState pending={saving > 0} saved={saving === 0 && mutation.isSuccess} />
              <Switch label="Pause all" checked={s.paused} onChange={(paused) => save({ paused })} />
            </>
          )
        }
      >
        {q.error && <ErrorBox error={q.error} className="m-4" />}
        {q.isLoading && <PageSpinner />}
        {s && (
          <>
            {(!s.emailConfigured || s.paused) && (
              <div className="flex flex-col gap-2 px-4 pt-4">
                {s.paused && (
                  <Callout tone="warning" title="Email notifications are paused" icon={<MailX className="mt-0.5 size-4 shrink-0 text-warning" />}>
                    Nothing is emailed while paused, and nothing piles up for later. Turn “Pause all” off to resume.
                  </Callout>
                )}
                {!s.emailConfigured && (
                  <Callout title="Email isn't set up on this server yet">
                    No SMTP server is configured, so emails are not delivered. Your choices are kept for when an administrator sets it up.
                  </Callout>
                )}
              </div>
            )}
            <div className={cn("overflow-x-auto px-1 pt-1", s.paused && "opacity-60")}>
              <table className={table}>
                <thead>
                  <tr>
                    <th scope="col">Notify me about</th>
                    {deliveries.map((d) => (
                      <th key={d.value} scope="col" title={d.hint} className="w-16 !px-1 !text-center sm:w-24">
                        {d.label}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {s.preferences.map((p) => (
                    <tr key={p.kind}>
                      <td>
                        <p className="font-medium">{p.label}</p>
                        <p className="text-[13px] text-muted">{p.description}</p>
                      </td>
                      {deliveries.map((d) => (
                        <td key={d.value} className="!px-1 text-center">
                          {p.deliveries && !p.deliveries.includes(d.value) ? (
                            // Request updates go out immediately or not at all (never in a digest).
                            <span className="text-faint" title={`${p.label} are not sent in the digest`}>
                              <span aria-hidden>—</span>
                              <span className="sr-only">{`${p.label}: ${d.label} is not available`}</span>
                            </span>
                          ) : (
                            <input
                              type="radio"
                              name={`email-${p.kind}`}
                              value={d.value}
                              aria-label={`${p.label}: ${d.label}`}
                              title={d.value === p.defaultDelivery ? `${d.label} (default)` : d.label}
                              checked={p.delivery === d.value}
                              onChange={() => save(preferencePatch(p.kind, d.value))}
                              className="size-4 cursor-pointer accent-accent"
                            />
                          )}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {s.preferences.some((p) => !p.deliveries || p.deliveries.includes("digest")) && (
              <div className="flex flex-col gap-3 border-t border-line p-4">
                <div className="flex flex-wrap items-end gap-3">
                  <Field label="Daily digest at" className="w-32">
                    {(id) => (
                      <Select id={id} value={s.digestHour} onChange={(e) => save({ digestHour: Number(e.target.value) })}>
                        {Array.from({ length: 24 }, (_, h) => (
                          <option key={h} value={h}>
                            {formatHour(h)}
                          </option>
                        ))}
                      </Select>
                    )}
                  </Field>
                  <Field label="Time zone" className="w-64 max-w-full">
                    {(id) => (
                      <Select id={id} value={s.timeZone} onChange={(e) => save({ timeZone: e.target.value })}>
                        {zones.map((z) => (
                          <option key={z} value={z}>
                            {z.replace(/_/g, " ")}
                          </option>
                        ))}
                      </Select>
                    )}
                  </Field>
                  {here !== s.timeZone && (
                    <Button variant="ghost" size="sm" className="mb-0.5" onClick={() => save({ timeZone: here })}>
                      Use {here.replace(/_/g, " ")}
                    </Button>
                  )}
                </div>
                <div className="flex flex-col gap-1 text-[13px] text-muted">
                  <p>
                    Next digest {nextDigestLabel(s.nextDigestAt, s.timeZone)} if there is anything to send
                    {s.lastDigestAt && (
                      <>
                        {" "}
                        · last one sent <When iso={s.lastDigestAt} />
                      </>
                    )}
                    .
                  </p>
                  <p>“Immediately” waits a couple of minutes so several changes to one issue arrive as one email. Every email has a one-click unsubscribe link.</p>
                </div>
              </div>
            )}
          </>
        )}
      </SettingsSection>
    </div>
  );
}

function SaveState({ pending, saved }: { pending: boolean; saved: boolean }) {
  if (pending) return <span className="text-[13px] text-muted">Saving…</span>;
  if (saved)
    return (
      <span className="inline-flex items-center gap-1 text-[13px] text-muted" role="status">
        <Check className="size-3.5 text-success" /> Saved
      </span>
    );
  return null;
}
