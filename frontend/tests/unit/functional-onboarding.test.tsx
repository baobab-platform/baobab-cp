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
  it("round-trips a saved incorporated multi-market draft without erasing evidence or hidden requirements", async () => {
    const saved = {
      client_application_id: "capp_test123", reference: "APP-2026-000099",
      status: "DRAFT", version: 6,
      business_identity: {
        operating_name: "Example Logistics", organisation_form: "COMPANY",
        operating_country: "UG", incorporation_claim: "INCORPORATED_CLAIMED",
        legal_name: "Example Logistics (Pty) Ltd", jurisdiction_of_incorporation: "ZA",
        registration_identifiers: [
          { type: "COMPANY_REGISTRATION", value: "2026/000001/07", issuing_jurisdiction: "ZA" },
          { type: "TAX", value: "TAX-REFERENCE", issuing_jurisdiction: "ZA" },
        ],
        trading_names: ["Example Cargo"],
        authorised_representative: { full_name: "Example Person", role: "Director", email: "person@example.test" },
      },
      requirements: { operates_b2b: true, requires_accounting: true, requires_procurement: true, notes: "Keep this note" },
      requested_markets: [
        { country_code: "ZA", note: "Source market" },
        { country_code: "UG", note: "Destination market" },
      ],
    };
    mockRead.mockResolvedValue(saved);
    mockWrite.mockResolvedValue({ ...saved, version: 7 });
    render(<ApplicantWorkspace />);
    fireEvent.change(screen.getByLabelText("Control Plane application ID"), { target: { value: "capp_test123" } });
    fireEvent.click(screen.getByRole("button", { name: "Load my draft" }));
    await waitFor(() => expect((screen.getByLabelText("Declared company registration number") as HTMLInputElement).value)
      .toBe("2026/000001/07"));
    expect((screen.getByLabelText("Jurisdiction of incorporation") as HTMLSelectElement).value).toBe("ZA");
    expect((screen.getByLabelText("Operating country") as HTMLSelectElement).value).toBe("UG");
    expect((screen.getByLabelText("Request Uganda") as HTMLInputElement).checked).toBe(true);
    expect((screen.getByLabelText("Request South Africa") as HTMLInputElement).checked).toBe(true);
    expect((screen.getByLabelText("Sell to other businesses") as HTMLInputElement).checked).toBe(true);
    expect((screen.getByLabelText("Manage accounting") as HTMLInputElement).checked).toBe(true);
    fireEvent.change(screen.getByLabelText("Operating business name"), { target: { value: "Example Logistics Revised" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft changes" }));
    await waitFor(() => expect(mockWrite).toHaveBeenCalledTimes(1));
    const command = mockWrite.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(command.action).toBe("update");
    const body = command.body as Record<string, unknown>;
    expect(body.version).toBe(6);
    expect(body.requested_markets).toEqual(saved.requested_markets);
    expect(body.requirements).toMatchObject({
      operates_b2b: true, requires_accounting: true,
      requires_procurement: true, notes: "Keep this note",
    });
    const identity = body.business_identity as Record<string, unknown>;
    expect(identity.operating_country).toBe("UG");
    expect(identity.jurisdiction_of_incorporation).toBe("ZA");
    expect(identity.registration_identifiers).toEqual(saved.business_identity.registration_identifiers);
    expect(identity.trading_names).toEqual(["Example Cargo"]);
    expect(identity.authorised_representative).toMatchObject({
      full_name: "Example Person", role: "Director", email: "person@example.test",
    });
    expect(body).not.toHaveProperty("legal_entity_id");
    expect(body).not.toHaveProperty("verification_state");
  });

  it("requires at least one requested market rather than treating residence as permission", async () => {
    render(<ApplicantWorkspace />);
    fireEvent.change(screen.getByLabelText("Operating business name"), { target: { value: "Example" } });
    fireEvent.change(screen.getByLabelText("Authorised representative’s full name"), { target: { value: "Test Owner" } });
    fireEvent.click(screen.getByLabelText("Request South Africa"));
    fireEvent.click(screen.getByRole("button", { name: "Create application draft" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("Select at least one requested market"));
    expect(mockWrite).not.toHaveBeenCalled();
  });

});
