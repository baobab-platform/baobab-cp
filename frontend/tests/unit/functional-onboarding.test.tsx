// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApplicantWorkspace } from "@/features/onboarding/ApplicantWorkspace";
import { ReviewerWorkspace } from "@/features/onboarding/ReviewerWorkspace";
import { OperationWorkspace } from "@/features/onboarding/OperationWorkspace";

const mockRead = vi.fn();
const mockWrite = vi.fn();
vi.mock("@/features/onboarding/workspace-client", () => ({
  readWorkspace: (...args: unknown[]) => mockRead(...args),
  sendWorkspaceCommand: (...args: unknown[]) => mockWrite(...args),
}));

describe("PEO-06 functional Console and authority separation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("allows an unincorporated business to draft without manufactured company registration", async () => {
    mockWrite.mockResolvedValue({
      client_application_id: "capp_test123", reference: "APP-2026-000001", status: "DRAFT", version: 1,
    });
    render(<ApplicantWorkspace />);
    fireEvent.change(screen.getByLabelText("Operating business name"), { target: { value: "Example Trading Business" } });
    fireEvent.change(screen.getByLabelText("Authorised representative’s full name"), { target: { value: "Test Applicant" } });
    fireEvent.click(screen.getByRole("button", { name: "Create application draft" }));
    await waitFor(() => expect(mockWrite).toHaveBeenCalledTimes(1));
    const cmd = mockWrite.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(cmd.action).toBe("create");
    const body = cmd.body as Record<string, unknown>;
    const identity = body.business_identity as Record<string, unknown>;
    expect(identity.incorporation_claim).toBe("NOT_INCORPORATED");
    expect(identity.registration_identifiers).toBeUndefined();
    expect(body).not.toHaveProperty("legal_entity_id");
    expect(body).not.toHaveProperty("applicant_principal_id");
    expect(screen.getByText(/saving is not admission approval/i)).toBeTruthy();
  });

  it("does not offer an applicant an approval or tenant-activation action", () => {
    render(<ApplicantWorkspace />);
    expect(screen.queryByRole("button", { name: /approve|activate|verify company/i })).toBeNull();
    expect(screen.getByText(/different, qualified reviewer/i)).toBeTruthy();
  });

  it("limits reviewer display to current submitted applications supplied by CP", async () => {
    mockRead.mockResolvedValue([
      { client_application_id: "capp_test123", reference: "APP-2026-000001", status: "SUBMITTED" },
    ]);
    render(<ReviewerWorkspace />);
    fireEvent.click(screen.getByRole("button", { name: "Load my authorised review queue" }));
    await waitFor(() => expect(screen.getByText("APP-2026-000001")).toBeTruthy());
    expect(mockRead).toHaveBeenCalledWith("review-queue");
    expect(screen.getByText(/a successful stage does not authorise the next stage/i)).toBeTruthy();
  });

  it("keeps provisioning status read only, not a locally-set activation", async () => {
    mockRead.mockResolvedValue({ operation_id: "op_123456", status: "PROVISIONING" });
    render(<OperationWorkspace />);
    fireEvent.change(screen.getByLabelText("Control Plane operation ID"), { target: { value: "op_123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Refresh operation state" }));
    await waitFor(() => expect(screen.getByText("PROVISIONING")).toBeTruthy());
    expect(mockRead).toHaveBeenCalledWith("operation", "op_123456");
    expect(screen.queryByRole("button", { name: /activate tenant/i })).toBeNull();
  });
});
