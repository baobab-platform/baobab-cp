import type { InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";

import { cx } from "../classes";
import styles from "./controls.module.css";

export function TextInput({ className, type = "text", ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...rest} type={type} className={cx(styles.control, className)} />;
}

export function Select({ className, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...rest} className={cx(styles.control, className)} />;
}

export function Textarea({ className, rows = 4, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...rest} rows={rows} className={cx(styles.control, styles.textarea, className)} />;
}
