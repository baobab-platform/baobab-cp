"use client";

import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import styles from "./Tabs.module.css";

export interface Tab {
  id: string;
  label: string;
  content: ReactNode;
}

export interface TabsProps {
  label: string;
  tabs: Tab[];
  initialTab?: string;
}

/**
 * ARIA tabs (WAI-ARIA Authoring Practices): one tab stop, arrow keys move
 * between tabs, Home and End jump to the ends, and selection follows focus.
 */
export function Tabs({ label, tabs, initialTab }: TabsProps) {
  const [selected, setSelected] = useState(initialTab ?? tabs[0]?.id);
  const refs = useRef<Array<HTMLButtonElement | null>>([]);
  const base = useId();

  const select = (index: number) => {
    const tab = tabs[index];
    if (!tab) return;
    setSelected(tab.id);
    refs.current[index]?.focus();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const last = tabs.length - 1;
    const next: Record<string, number> = {
      ArrowRight: index === last ? 0 : index + 1,
      ArrowLeft: index === 0 ? last : index - 1,
      Home: 0,
      End: last,
    };
    const target = next[event.key];
    if (target !== undefined) {
      event.preventDefault();
      select(target);
    }
  };

  return (
    <div>
      <div role="tablist" aria-label={label} className={styles.list}>
        {tabs.map((tab, index) => {
          const active = tab.id === selected;
          return (
            <button
              key={tab.id}
              ref={(element) => {
                refs.current[index] = element;
              }}
              type="button"
              role="tab"
              id={`${base}-tab-${tab.id}`}
              aria-selected={active}
              aria-controls={`${base}-panel-${tab.id}`}
              tabIndex={active ? 0 : -1}
              className={styles.tab}
              onClick={() => select(index)}
              onKeyDown={(event) => onKeyDown(event, index)}
            >
              {tab.label}
            </button>
          );
        })}
      </div>
      {tabs.map((tab) => (
        <div
          key={tab.id}
          role="tabpanel"
          id={`${base}-panel-${tab.id}`}
          aria-labelledby={`${base}-tab-${tab.id}`}
          hidden={tab.id !== selected}
          tabIndex={0}
          className={styles.panel}
        >
          {tab.content}
        </div>
      ))}
    </div>
  );
}
