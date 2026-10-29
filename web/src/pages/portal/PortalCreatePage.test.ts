import { describe, expect, it } from "vitest";
import type { PortalFormField } from "@/features/servicedesk/api";
import { formValues, visibleFields } from "./PortalCreatePage";

const field = (key: string, type: PortalFormField["type"], showWhen?: PortalFormField["showWhen"]): PortalFormField => ({ key, label: key, type, required: true, showWhen });

const fields: PortalFormField[] = [
  field("category", "select"),
  field("system", "select", { field: "category", values: ["Access"] }),
  field("justification", "text", { field: "system", values: ["prod"] }),
  field("urgent", "checkbox"),
  field("wait", "text", { field: "urgent", values: ["false"] }),
  field("teams", "multiselect"),
  field("ref", "text", { field: "teams", values: ["Payments"] }),
];

describe("visibleFields", () => {
  it("shows a field while the field it tests is shown and matches", () => {
    expect([...visibleFields(fields, { category: "Hardware", system: "prod" })]).toEqual(["category", "urgent", "wait", "teams"]);
    expect(visibleFields(fields, { category: "Access", system: "prod" }).has("justification")).toBe(true);
  });
  it("treats an unticked checkbox as false and matches any multiselect choice", () => {
    expect(visibleFields(fields, { urgent: true }).has("wait")).toBe(false);
    expect(visibleFields(fields, { teams: ["Platform", "Payments"] }).has("ref")).toBe(true);
  });
});

describe("formValues", () => {
  it("leaves hidden fields out", () => {
    expect(formValues(fields, { category: "Hardware", system: "prod", justification: "x", wait: "later" })).toEqual({ category: "Hardware", wait: "later" });
  });
});
