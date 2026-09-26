import type { ReactNode } from "react";

import styles from "./Table.module.css";

export type SortDirection = "ascending" | "descending";

export interface Column<Row> {
  key: string;
  header: string;
  cell: (row: Row) => ReactNode;
  /** When set, the header links to the server-sorted page (prompt section 55). */
  sortHref?: string;
  numeric?: boolean;
}

export interface TableProps<Row> {
  /** Names the table for assistive technology (WCAG 1.3.1). */
  caption: string;
  hideCaption?: boolean;
  columns: Array<Column<Row>>;
  rows: Row[];
  rowKey: (row: Row) => string;
  /** The current server sort, reflected with aria-sort. */
  sort?: { key: string; direction: SortDirection };
  /** Shown when rows is empty; should teach, e.g. an EmptyState. */
  empty: ReactNode;
}

/**
 * A semantic, server-driven table. Sorting, filtering and pagination
 * happen in the Control Plane query, never by loading everything into the
 * browser; a sortable header is a link to the next server state.
 */
export function Table<Row>({ caption, hideCaption = false, columns, rows, rowKey, sort, empty }: TableProps<Row>) {
  return (
    <div className={styles.scroller}>
      <table className={styles.table}>
        <caption className={hideCaption ? "visually-hidden" : styles.caption}>{caption}</caption>
        <thead>
          <tr>
            {columns.map((column) => {
              const sorted = sort?.key === column.key ? sort.direction : undefined;
              return (
                <th key={column.key} scope="col" aria-sort={sorted} className={column.numeric ? styles.numeric : undefined}>
                  {column.sortHref ? <a href={column.sortHref}>{column.header}</a> : column.header}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={columns.length}>{empty}</td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr key={rowKey(row)}>
                {columns.map((column) => (
                  <td key={column.key} className={column.numeric ? styles.numeric : undefined}>
                    {column.cell(row)}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
