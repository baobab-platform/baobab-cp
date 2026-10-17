"use client";

import type { ReactNode } from "react";
import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import styles from "./Dialog.module.css";

export interface DialogProps {
  open: boolean;
  /** Closing changes presentation only; the caller retains command authority. */
  onClose: () => void;
  title: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
}

/**
 * Controlled Base UI modal. Portal isolation, Escape, focus trapping and
 * restoration share the primitive's lifecycle, including external openers.
 * Existing confirmation/reason checks and backend authorisation remain owned
 * by their callers; dismissal never invokes a privileged command.
 */
export function Dialog({ open, onClose, title, description, children, footer }: DialogProps) {
  return <BaseDialog.Root open={open} onOpenChange={(next) => { if (!next) onClose(); }}>
    <BaseDialog.Portal>
      <BaseDialog.Backdrop className={styles.backdrop} />
      <BaseDialog.Popup className={styles.dialog} finalFocus>
        <BaseDialog.Title className={styles.title}>{title}</BaseDialog.Title>
        {description ? <BaseDialog.Description className={styles.description}>{description}</BaseDialog.Description> : null}
        {children}
        {footer ? <div className={styles.footer}>{footer}</div> : null}
      </BaseDialog.Popup>
    </BaseDialog.Portal>
  </BaseDialog.Root>;
}
