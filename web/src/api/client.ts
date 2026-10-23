import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];
export type Problem = Schemas["Problem"];

/** ApiError carries an RFC 9457 problem from the API. */
export class ApiError extends Error {
  status: number;
  problem: Problem;
  constructor(problem: Problem) {
    super(problem.detail || problem.title || `HTTP ${problem.status}`);
    this.status = problem.status;
    this.problem = problem;
  }
  get code() {
    return this.problem.code;
  }
  /** Field-level messages keyed by field name. */
  fieldErrors(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const e of this.problem.errors ?? []) if (e.field) out[e.field] = e.message;
    return out;
  }
}

// Cookie sessions need a header a cross-site form cannot send (the API's CSRF rule).
const csrf: Middleware = {
  onRequest({ request }) {
    request.headers.set("X-Requested-With", "XMLHttpRequest");
    return request;
  },
};

export const api = createClient<paths>({ baseUrl: "/api/v1", credentials: "same-origin" });
api.use(csrf);

type Result<T> = { data?: T; error?: unknown; response: Response };

/** unwrap turns an openapi-fetch result into data or a thrown ApiError. */
export async function unwrap<T>(p: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await p;
  if (!response.ok) {
    const problem = (error && typeof error === "object" ? error : {}) as Partial<Problem>;
    throw new ApiError({ title: response.statusText, status: response.status, ...problem } as Problem);
  }
  return data as T;
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const fields = err.problem.errors?.map((e) => `${e.field}: ${e.message}`);
    return fields?.length && !err.problem.detail ? fields.join("; ") : err.message;
  }
  if (err instanceof Error) return err.message;
  return String(err);
}
