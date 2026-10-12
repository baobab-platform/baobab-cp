import {notFound} from "next/navigation";
import {invokeProgressive} from "@/server/bff/progressive";
import {authenticatedConsoleActor,hasExactScopedPermission} from "@/server/console/scoped-context";
import {DatasetOperationsWorkbench} from "@/features/console/DatasetOperationsWorkbench";
import {datasetConsoleEnabled} from "@/server/bff/console-datasets";

export const dynamic="force-dynamic";
export const revalidate=0;
export default async function DatasetWorkbenchPage({params}:{params:Promise<{organisationId:string}>}){
 if(!datasetConsoleEnabled())notFound();
 const {organisationId}=await params;
 if(!/^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(organisationId))notFound();
 const actor=await authenticatedConsoleActor();
 const current=await invokeProgressive({action:"effective-authority"},actor.accessToken).then(r=>r.data).catch(()=>null);
 const can=(p:string)=>hasExactScopedPermission(current,actor.principalId,p,"organisation",organisationId);
 if(!can("organisation.view"))notFound();
 const canExport=can("organisation.export"),canBulk=can("organisation.bulk.propose")&&can("organisation.manage"),canApprove=can("organisation.bulk.approve")||canExport;
 if(!canExport&&!canBulk&&!canApprove)notFound();
 return <main className="mx-auto max-w-4xl space-y-6 p-6">
  <h1 className="text-3xl font-bold">Dataset operations</h1>
  <p className="text-sm text-muted-foreground">Organisation: {organisationId}. The Control Plane independently authorises every command, approval, execution and download.</p>
  <DatasetOperationsWorkbench organisationId={organisationId} csrfToken={actor.csrfToken}
   canExport={canExport} canBulk={canBulk} canApprove={canApprove}/>
 </main>;
}
