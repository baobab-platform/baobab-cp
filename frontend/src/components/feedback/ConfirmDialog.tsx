"use client";

import { useId, useState } from "react";

import { format, messages } from "@/lib/i18n/messages";

import { Field } from "../forms/Field";
import { TextInput, Textarea } from "../forms/controls";
import { Button } from "../primitives/Button";
import { Dialog } from "./Dialog";

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  /** Receives the reason when one is required, otherwise undefined. */
  onConfirm: (reason: string | undefined) => void;
  title: string;
  /** The consequence, stated plainly (prompt section 88). */
  consequence: string;
  confirmLabel: string;
  /** For the highest-risk actions: the phrase the user must type, e.g. the tenant's name. */
  typedConfirmation?: string;
  /** Require a reason, recorded in the audit trail. */
  requireReason?: boolean;
  busy?: boolean;
}

/**
 * Deliberate confirmation for a destructive action. The confirm button is
 * enabled only once the typed phrase matches and any required reason is
 * given; the backend still decides whether the action is allowed.
 */
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  consequence,
  confirmLabel,
  typedConfirmation,
  requireReason = false,
  busy = false,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState("");
  const [reason, setReason] = useState("");
  const id = useId();
  const ready = (typedConfirmation === undefined || typed === typedConfirmation) && (!requireReason || reason.trim() !== "");

  const close = () => {
    setTyped("");
    setReason("");
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      title={title}
      description={consequence}
      footer={
        <>
          <Button onClick={close}>{messages.cancel}</Button>
          <Button variant="danger" busy={busy} disabled={!ready} onClick={() => onConfirm(requireReason ? reason.trim() : undefined)}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {requireReason ? (
        <Field id={`${id}-reason`} label={messages.reasonLabel} hint={messages.reasonHint} required>
          {(control) => <Textarea {...control} value={reason} onChange={(event) => setReason(event.target.value)} />}
        </Field>
      ) : null}
      {typedConfirmation !== undefined ? (
        <Field id={`${id}-typed`} label={format(messages.typedConfirmationLabel, { phrase: typedConfirmation })} required>
          {(control) => (
            <TextInput {...control} autoComplete="off" spellCheck={false} value={typed} onChange={(event) => setTyped(event.target.value)} />
          )}
        </Field>
      ) : null}
    </Dialog>
  );
}
