import type { Schemas } from "./client";

export type Me = Schemas["Me"];
export type User = Schemas["User"];
export type Project = Schemas["Project"];
export type ProjectTemplate = Schemas["ProjectTemplate"];
export type Issue = Schemas["Issue"];
export type IssueDetail = Schemas["IssueDetail"];
export type IssueType = Schemas["IssueType"];
export type Status = Schemas["Status"];
export type StatusCategory = Schemas["StatusCategory"];
export type Priority = Schemas["Priority"];
export type OrgRole = Schemas["OrgRole"];
export type Permission = string;

export const PRIORITIES: Priority[] = ["highest", "high", "medium", "low", "lowest"];
