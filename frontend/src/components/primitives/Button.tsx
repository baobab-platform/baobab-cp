import type { ButtonHTMLAttributes } from "react";

import { cx } from "../classes";
import styles from "./Button.module.css";

export type ButtonVariant = "primary" | "secondary" | "danger" | "ghost";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  /** The command is running: the button is disabled and announces it. */
  busy?: boolean;
}

/** An owned button. It defaults to type="button" so it never submits a form by accident. */
export function Button({ variant = "secondary", busy = false, type = "button", disabled, className, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      type={type}
      className={cx(styles.button, styles[variant], className)}
      disabled={disabled === true || busy}
      aria-busy={busy || undefined}
    />
  );
}
