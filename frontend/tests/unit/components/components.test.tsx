// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import {
  Alert,
  Button,
  EmptyState,
  ErrorState,
  Field,
  LoadingState,
  PageHeader,
  SideNav,
  StatusBadge,
  statusTones,
  Table,
  Tabs,
  TextInput,
} from "@/components";

import { expectAccessible } from "../axe";

describe("Button", () => {
  it("never submits a form by default and announces a busy command", async () => {
    const { container } = render(
      <form>
        <Button>Save draft</Button>
        <Button busy variant="primary">
          Submitting…
        </Button>
      </form>,
    );
    const [save, busy] = screen.getAllByRole("button");
    expect(save).toHaveProperty("type", "button");
    expect(busy?.getAttribute("aria-busy")).toBe("true");
    expect(busy).toHaveProperty("disabled", true);
    await expectAccessible(container);
  });
});

describe("Field", () => {
  it("labels the control and ties hint and error to it", async () => {
    const { container } = render(
      <Field id="legal-name" label="Legal name" hint="As registered." error="Enter the legal name." required>
        {(control) => <TextInput {...control} />}
      </Field>,
    );
    const input = screen.getByLabelText(/Legal name/);
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(input.getAttribute("aria-describedby")).toBe("legal-name-hint legal-name-error");
    expect(input).toHaveProperty("required", true);
    expect(screen.getByText("Enter the legal name.").id).toBe("legal-name-error");
    await expectAccessible(container);
  });

  it("marks nothing invalid without an error", () => {
    render(<Field id="n" label="Name">{(control) => <TextInput {...control} />}</Field>);
    const input = screen.getByLabelText("Name");
    expect(input.hasAttribute("aria-invalid")).toBe(false);
    expect(input.hasAttribute("aria-describedby")).toBe(false);
  });
});

describe("StatusBadge", () => {
  it("renders every tone with text, never colour alone", async () => {
    const { container } = render(
      <p>
        {statusTones.map((tone) => (
          <StatusBadge key={tone} tone={tone} />
        ))}
        <StatusBadge tone="in-progress" label="Awaiting authorisation" />
      </p>,
    );
    expect(screen.getByText("Awaiting authorisation")).toBeTruthy();
    expect(screen.getByText("Revoked")).toBeTruthy();
    for (const badge of container.querySelectorAll("[data-tone]")) {
      expect(badge.textContent?.trim().length).toBeGreaterThan(1);
    }
    await expectAccessible(container);
  });
});

describe("Alert and states", () => {
  it("announces only when asked, assertively only for failures", async () => {
    const { container } = render(
      <div>
        <Alert tone="informational" title="Quiet" />
        <Alert tone="success" title="Saved" announce="polite" />
        <Alert tone="failure" title="Could not save" announce="assertive" />
      </div>,
    );
    expect(screen.getByRole("status").textContent).toContain("Saved");
    expect(screen.getByRole("alert").textContent).toContain("Could not save");
    await expectAccessible(container);
  });

  it("gives loading a context, empty states an explanation and errors a next step and reference", async () => {
    const { container } = render(
      <main>
        <LoadingState message="Loading onboarding requests…" />
        <EmptyState title="No requests yet" explanation="Requests appear after an approval." />
        <ErrorState title="Could not authorise" detail="You requested this." nextStep="Ask another authoriser." reference="abc-123" />
      </main>,
    );
    expect(screen.getByRole("status").textContent).toContain("Loading onboarding requests…");
    const error = screen.getByRole("alert");
    expect(error.textContent).toContain("Ask another authoriser.");
    expect(error.textContent).toContain("abc-123");
    await expectAccessible(container);
  });
});

describe("Table", () => {
  const columns = [
    { key: "name", header: "Organisation", cell: (row: { id: string; name: string }) => row.name, sortHref: "?sort=name" },
    { key: "id", header: "Reference", cell: (row: { id: string; name: string }) => row.id },
  ];

  it("is a captioned table whose server sort is exposed with aria-sort", async () => {
    const { container } = render(
      <Table
        caption="Onboarding requests"
        columns={columns}
        rows={[{ id: "tor_1", name: "Example A" }]}
        rowKey={(row) => row.id}
        sort={{ key: "name", direction: "ascending" }}
        empty={null}
      />,
    );
    expect(screen.getByRole("table", { name: "Onboarding requests" })).toBeTruthy();
    const header = screen.getByRole("columnheader", { name: "Organisation" });
    expect(header.getAttribute("aria-sort")).toBe("ascending");
    expect(screen.getByRole("link", { name: "Organisation" }).getAttribute("href")).toBe("?sort=name");
    await expectAccessible(container);
  });

  it("renders the teaching empty state across all columns", () => {
    render(
      <Table caption="Requests" columns={columns} rows={[]} rowKey={(row) => row.id} empty={<p>Requests appear after an approval.</p>} />,
    );
    const cell = screen.getByText("Requests appear after an approval.").closest("td");
    expect(cell?.getAttribute("colspan")).toBe("2");
  });
});

describe("Navigation", () => {
  it("labels the landmark and marks the current page", async () => {
    const { container } = render(
      <div>
        <SideNav
          label="Primary"
          current="/admission"
          items={[
            { href: "/applications", label: "Applications" },
            { href: "/admission", label: "Admission" },
          ]}
        />
        <PageHeader title="Admission" />
      </div>,
    );
    expect(screen.getByRole("navigation", { name: "Primary" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Admission" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Applications" }).getAttribute("aria-current")).toBeNull();
    await expectAccessible(container);
  });

  it("moves between tabs with the arrow keys, Home and End", async () => {
    const user = userEvent.setup();
    const { container } = render(
      <Tabs
        label="Request"
        tabs={[
          { id: "summary", label: "Summary", content: <p>Summary panel</p> },
          { id: "history", label: "History", content: <p>History panel</p> },
          { id: "audit", label: "Audit", content: <p>Audit panel</p> },
        ]}
      />,
    );
    await expectAccessible(container);
    const summary = screen.getByRole("tab", { name: "Summary" });
    expect(summary.getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("tabpanel").textContent).toBe("Summary panel");

    await user.click(summary);
    await user.keyboard("{ArrowRight}");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "History" }));
    expect(screen.getByRole("tabpanel").textContent).toBe("History panel");

    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "Audit" }).getAttribute("aria-selected")).toBe("true");
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Summary" }).getAttribute("aria-selected")).toBe("true");
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "Audit" }).getAttribute("aria-selected")).toBe("true");
    await user.keyboard("{Home}");
    expect(screen.getByRole("tab", { name: "Summary" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getAllByRole("tab").filter((tab) => tab.tabIndex === 0)).toHaveLength(1);
  });
});
