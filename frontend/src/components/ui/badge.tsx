import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

const badgeVariants = cva("inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-semibold leading-none", {
 variants: { variant: {
   neutral: "border-border bg-muted text-muted-foreground",
   informational: "border-status-informational bg-status-informational-bg text-status-informational-fg",
   success: "border-status-success bg-status-success-bg text-status-success-fg",
   warning: "border-status-warning bg-status-warning-bg text-status-warning-fg",
   blocked: "border-status-blocked bg-status-blocked-bg text-status-blocked-fg",
   failure: "border-status-failure bg-status-failure-bg text-status-failure-fg",
   expired: "border-status-expired bg-status-expired-bg text-status-expired-fg",
   revoked: "border-status-revoked bg-status-revoked-bg text-status-revoked-fg",
 } }, defaultVariants: { variant: "neutral" } });

export function Badge({ className, variant, ...props }: ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
 return <span data-slot="badge" className={cn(badgeVariants({ variant }), className)} {...props} />;
}
