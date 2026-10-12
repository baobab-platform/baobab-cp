import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export const buttonVariants = cva(
  "inline-flex min-h-11 items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  { variants: {
    variant: {
      default: "bg-primary px-5 text-primary-foreground hover:bg-primary-hover",
      secondary: "border border-border bg-card px-5 text-foreground hover:bg-muted",
      outline: "border border-border-strong bg-transparent px-5 text-foreground hover:bg-muted",
      ghost: "px-4 text-foreground hover:bg-muted",
      link: "min-h-0 px-0 text-link underline-offset-4 hover:underline",
      gold: "bg-brand-gold px-5 text-brand-ink hover:bg-brand-gold/90",
      destructive: "bg-destructive px-5 text-destructive-foreground hover:bg-destructive/90",
    },
    size: {
      default: "h-11",
      sm: "h-9 min-h-9 px-3",
      lg: "h-12 min-h-12 px-7",
      icon: "size-11 min-h-11 px-0",
    },
  }, defaultVariants: { variant: "default", size: "default" } }
);

export type ButtonProps = ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & { asChild?: boolean };

/** Presentation primitive: never substitutes CP authorisation. Safe type=button default. */
export function Button({ className, variant, size, asChild = false, type = "button", ...props }: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp data-slot="button" className={cn(buttonVariants({ variant, size, className }))} {...(!asChild ? { type } : {})} {...props} />
  );
}
