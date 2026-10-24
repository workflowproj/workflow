import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MutationCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { makeRouter } from "@/router";
import { ConfirmProvider, ToastProvider } from "@/components/ui";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
      // Client errors will not fix themselves; retry only transient failures.
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
  mutationCache: new MutationCache({
    onError: (err, _vars, _ctx, mutation) => {
      // A session that expired mid-use: send people to sign in again. Sign-in forms (meta.signIn)
      // show their own 401s (a wrong password or code).
      if (err instanceof ApiError && err.status === 401 && !mutation.meta?.signIn) {
        window.location.assign(`/login?returnTo=${encodeURIComponent(window.location.pathname + window.location.search)}`);
      }
    },
  }),
});

const router = makeRouter(queryClient);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <ConfirmProvider>
          <RouterProvider router={router} />
        </ConfirmProvider>
      </ToastProvider>
    </QueryClientProvider>
  </StrictMode>,
);
