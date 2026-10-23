import { infiniteQueryOptions, keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";

export const PAGE_SIZE = 50;

export const searchMetadataQuery = queryOptions({
  queryKey: ["search-metadata"],
  queryFn: () => unwrap(api.GET("/search/fields")),
  staleTime: 5 * 60_000,
});

export const filtersQuery = queryOptions({
  queryKey: ["filters"],
  queryFn: () => unwrap(api.GET("/filters")),
  staleTime: 30_000,
});

/** searchResultsQuery pages through GET /search; keys share the ["search", jql] prefix. */
export const searchResultsQuery = (jql: string) =>
  infiniteQueryOptions({
    queryKey: ["search", jql, "navigator"],
    queryFn: ({ pageParam }) => unwrap(api.GET("/search", { params: { query: { jql, limit: PAGE_SIZE, offset: pageParam } } })),
    initialPageParam: 0,
    getNextPageParam: (last) => (last.offset + last.issues.length < last.total ? last.offset + last.issues.length : undefined),
    placeholderData: keepPreviousData,
    retry: false,
  });
