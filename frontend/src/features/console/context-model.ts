/** An option is an exact-scope hint obtained from CP's caller-bound authority,
 * not proof that the underlying resource currently exists or is ACTIVE.
 */
export type ConsoleContextOption = {
  kind: "organisation" | "tenant";
  id: string;
  label: string;
};

export function contextHref(option: ConsoleContextOption): string {
  return option.kind === "organisation"
    ? "/workspace/organisations/" + encodeURIComponent(option.id)
    : "/workspace/tenants/" + encodeURIComponent(option.id);
}
