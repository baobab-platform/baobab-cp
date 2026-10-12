// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OperationWorkspace } from "@/features/onboarding/OperationWorkspace";

const mockRead = vi.fn();
vi.mock("@/features/onboarding/workspace-client", () => ({
  readWorkspace: (...args: unknown[]) => mockRead(...args),
}));

const tenant = "ten_1234567890";
const provisioningId = "tprov_9876543210";
const digest = "sha256:" + "a".repeat(64);
const record = {
  tenant_id: tenant, tenant_provisioning_id: provisioningId,
  state: "PLANNED", readiness_status: "BLOCKED", revision: 7,
  blocking_reasons: ["CERTIFICATION_PENDING"],
  current_plan: { plan_digest: digest, plan_version: 4, stale: false },
  approval: { decision: "APPROVED", plan_digest: digest },
};
const plan = { tenant_id: tenant, tenant_provisioning_id: provisioningId, plan_digest: digest };
const readiness = { tenant_id: tenant, tenant_provisioning_id: provisioningId, status: "BLOCKED", snapshots: [] };

async function display() {
  render(<OperationWorkspace />);
  fireEvent.change(screen.getByLabelText("Tenant ID"), { target: { value: tenant } });
  fireEvent.change(screen.getByLabelText("Tenant provisioning ID"), { target: { value: provisioningId } });
  fireEvent.click(screen.getByRole("button", { name: "Refresh lifecycle, immutable plan and readiness" }));
}

describe("PEO-06E current Control Plane provisioning provenance", () => {
  beforeEach(() => {
    vi.resetAllMocks();
  });

  it("shows a current revision, approved plan digest and blocked readiness separately", async () => {
    mockRead.mockImplementation(async (action: string) =>
      action === "provisioning" ? record : action === "provisioning-plan" ? plan : readiness);
    await display();
    await waitFor(() => expect(screen.getByText("BLOCKED")).toBeTruthy());
    expect(screen.getByText("PLANNED")).toBeTruthy();
    expect(screen.getByText("Yes — current digest")).toBeTruthy();
    expect(screen.getByText(digest)).toBeTruthy();
    expect(mockRead).toHaveBeenCalledWith("provisioning", provisioningId, tenant);
    expect(mockRead).toHaveBeenCalledWith("provisioning-plan", provisioningId, tenant);
    expect(mockRead).toHaveBeenCalledWith("provisioning-readiness", provisioningId, tenant);
    expect(screen.queryByRole("button", { name: /apply|activate/i })).toBeNull();
  });

  it("denies a stale/mismatched digest, rather than displaying misleading readiness", async () => {
    mockRead.mockImplementation(async (action: string) =>
      action === "provisioning" ? record :
        action === "provisioning-plan" ? { ...plan, plan_digest: "sha256:" + "b".repeat(64) } : readiness);
    await display();
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("could not be verified"));
    expect(screen.queryByText("Yes — current digest")).toBeNull();
    expect(screen.queryByText("PLANNED")).toBeNull();
  });

  it("rejects the wrong tenant even if plan digest matches", async () => {
    mockRead.mockImplementation(async (action: string) =>
      action === "provisioning" ? record :
        action === "provisioning-plan" ? { ...plan, tenant_id: "ten_other1234" } : readiness);
    await display();
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("could not be verified"));
    expect(screen.queryByText("PLANNED")).toBeNull();
  });

  it("treats absent plan/failed readiness independently from an operation success", async () => {
    mockRead.mockImplementation(async (action: string) =>
      action === "provisioning" ? record : action === "provisioning-plan" ? plan :
        Promise.reject(new Error("readiness unavailable")));
    await display();
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("could not be verified"));
    expect(screen.queryByText("Yes — current digest")).toBeNull();
  });

  it("shows operation progress and eventual resource state, never invented percent or activation", async () => {
    mockRead.mockResolvedValue({
      operation_id: "op_123456", operation_type: "TENANT_PROVISIONING",
      status: "SUCCEEDED", revision: 3, plan_digest: digest,
      progress: { completed_steps: 3, total_steps: 3 },
      result: { resource_type: "TENANT_PROVISIONING", resource_state: "READY" },
    });
    render(<OperationWorkspace />);
    fireEvent.change(screen.getByLabelText("Control Plane operation ID"), { target: { value: "op_123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Refresh operation state" }));
    await waitFor(() => expect(screen.getByText("SUCCEEDED")).toBeTruthy());
    expect(screen.getByText("READY")).toBeTruthy();
    expect(screen.getByText("3 / 3")).toBeTruthy();
    expect(screen.queryByText("ACTIVE")).toBeNull();
  });
});
