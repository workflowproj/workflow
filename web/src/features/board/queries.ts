import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";

export type Board = Schemas["Board"];
export type BoardData = Schemas["BoardData"];
export type BoardColumnData = Schemas["BoardColumnData"];
export type Sprint = Schemas["Sprint"];
export type Backlog = Schemas["Backlog"];
export type AvailableTransition = Schemas["AvailableTransition"];

/** The project's boards. Board settings changes also refresh ["board", id, ...] through events. */
export const boardsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["boards", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/boards", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

export const boardDataQuery = (boardId: string, jql: string) =>
  queryOptions({
    queryKey: ["board", boardId, jql],
    queryFn: () => unwrap(api.GET("/boards/{boardId}/data", { params: { path: { boardId }, query: { jql: jql || undefined } } })),
    placeholderData: keepPreviousData,
  });

export const backlogQuery = (boardId: string, jql: string) =>
  queryOptions({
    queryKey: ["backlog", boardId, jql],
    queryFn: () => unwrap(api.GET("/boards/{boardId}/backlog", { params: { path: { boardId }, query: { jql: jql || undefined } } })),
    placeholderData: keepPreviousData,
  });

/** Transitions the caller may run on an issue; nested under ["issue", key] so issue events refresh it. */
export const transitionsQuery = (issueKey: string) =>
  queryOptions({
    queryKey: ["issue", issueKey, "transitions"],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/transitions", { params: { path: { issueKey } } })),
    staleTime: 10_000,
  });

export const sprintsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["sprints", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/sprints", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

const storageKey = (projectKey: string) => `wfm.board.${projectKey}`;

function readStored(projectKey: string): string | null {
  try {
    return localStorage.getItem(storageKey(projectKey));
  } catch {
    return null;
  }
}

/**
 * useBoardSelection loads the project's boards and remembers which one this viewer last
 * picked (per browser); the board and backlog pages share the choice.
 */
export function useBoardSelection(projectKey: string) {
  const boards = useQuery(boardsQuery(projectKey));
  const [stored, setStored] = useState(() => readStored(projectKey));
  const list = boards.data ?? [];
  const board = list.find((b) => b.id === stored) ?? list[0];
  const select = (id: string) => {
    setStored(id);
    try {
      localStorage.setItem(storageKey(projectKey), id);
    } catch {
      // Storage can be unavailable (private windows); the choice then lasts for this visit.
    }
  };
  return { boards: list, board, select, isLoading: boards.isLoading, error: boards.error };
}
