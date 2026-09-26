import { PageHeader } from "@/components";

// The shell, sign-in and workspaces arrive in later gates
// (docs/frontend/fe-00-architecture-lock.md section 9). This page shows no
// data, so it cannot show invented data (prompt section 102).
export default function Home() {
  return (
    <main id="main" className="page">
      <PageHeader
        title="Baobab Control Plane Console"
        description="The Console is being built. No administrative functions are available yet."
      />
    </main>
  );
}
