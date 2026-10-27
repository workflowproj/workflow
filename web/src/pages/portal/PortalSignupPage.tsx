import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, LifeBuoy } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { providersQuery } from "@/api/queries";
import { Button, Field, Input, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ThemeMenu } from "@/layout/ThemeMenu";

const route = getRouteApi("/portal/signup/$projectKey");

/** PortalSignupPage lets anyone create a customer account for a help desk that allows it. */
export function PortalSignupPage() {
  const { projectKey } = route.useParams();
  const info = useQuery({
    queryKey: ["portal-signup", projectKey],
    queryFn: () => unwrap(api.GET("/portal/signup/{projectKey}", { params: { path: { projectKey } } })),
    retry: false,
  });
  const providers = useQuery(providersQuery);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const signup = useMutation({
    mutationFn: () => unwrap(api.POST("/portal/signup/{projectKey}", { params: { path: { projectKey } }, body: { email: email.trim(), displayName: name.trim() } })),
  });
  const fieldErrors = signup.error instanceof ApiError ? signup.error.fieldErrors() : {};
  return (
    <div className="flex min-h-full items-start justify-center bg-canvas px-4 py-12 sm:py-20">
      <main className="w-full max-w-md">
        <div className="mb-6 flex items-center gap-2 font-semibold">
          <span className="flex size-7 items-center justify-center rounded-md bg-accent text-accent-ink">
            <LifeBuoy aria-hidden className="size-4" />
          </span>
          Help centre
          <span className="ml-auto">
            <ThemeMenu />
          </span>
        </div>
        <div className="rounded-xl border border-line bg-surface p-6 shadow-sm">
          {info.isLoading || providers.isLoading ? (
            <PageSpinner />
          ) : !info.data ? (
            <>
              <h1 className="text-lg font-semibold">Sign-up is not available</h1>
              <p className="mt-2 text-sm text-muted">This help centre does not let people sign up. Ask the team for an invitation.</p>
            </>
          ) : providers.data && !providers.data.emailDelivery ? (
            <>
              <h1 className="text-lg font-semibold">Sign-up is not available</h1>
              <p className="mt-2 text-sm text-muted">
                Signing up emails you a link to choose a password, and this server doesn't send email yet. Ask {info.data.name} for an invitation.
              </p>
            </>
          ) : signup.isSuccess ? (
            <div role="status">
              <CheckCircle2 aria-hidden className="mb-2 size-6 text-success" />
              <h1 className="text-lg font-semibold">Check your email</h1>
              <p className="mt-2 text-sm text-muted">
                We sent a message to <strong className="text-ink">{email.trim()}</strong>. Follow its link to choose a password, then sign in to raise requests with{" "}
                {info.data.name}.
              </p>
            </div>
          ) : (
            <>
              <h1 className="text-lg font-semibold">Get help from {info.data.name}</h1>
              {info.data.description && <p className="mt-1 text-sm text-muted">{info.data.description}</p>}
              <form
                className="mt-5 flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  signup.mutate();
                }}
              >
                <Field label="Your name" required error={fieldErrors.displayName}>
                  {(id) => <Input id={id} autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} required />}
                </Field>
                <Field label="Email" required error={fieldErrors.email}>
                  {(id) => <Input id={id} type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />}
                </Field>
                {signup.error && !Object.keys(fieldErrors).length ? <ErrorBox error={signup.error} /> : null}
                <Button type="submit" variant="primary" loading={signup.isPending} disabled={!email.trim() || !name.trim()}>
                  Create account
                </Button>
              </form>
            </>
          )}
        </div>
        <p className="mt-4 text-center text-sm text-muted">
          Already have an account?{" "}
          <Link to="/login" search={{ returnTo: `/portal/${projectKey}` }} className="font-medium text-accent hover:underline">
            Sign in
          </Link>
        </p>
      </main>
    </div>
  );
}
