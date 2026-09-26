import { messages } from "@/lib/i18n/messages";

import styles from "./SkipLink.module.css";

/** The first focusable element on every page; pages render <main id="main">. */
export function SkipLink({ target = "main" }: { target?: string }) {
  return (
    <a className={styles.skip} href={`#${target}`}>
      {messages.skipToContent}
    </a>
  );
}
