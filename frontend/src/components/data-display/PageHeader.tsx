import type { ReactNode } from "react";

import styles from "./PageHeader.module.css";

export interface PageHeaderProps {
  title: string;
  /** What the page is for, in business language. */
  description?: string;
  actions?: ReactNode;
}

/** The page's single h1 and its primary actions. */
export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <header className={styles.header}>
      <div>
        <h1 className={styles.title}>{title}</h1>
        {description ? <p className={styles.description}>{description}</p> : null}
      </div>
      {actions ? <div className={styles.actions}>{actions}</div> : null}
    </header>
  );
}
