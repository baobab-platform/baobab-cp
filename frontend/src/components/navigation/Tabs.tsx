"use client";

import type { ReactNode } from "react";
import { Tabs as BaseTabs } from "@base-ui/react/tabs";
import styles from "./Tabs.module.css";

export interface Tab { id: string; label: string; content: ReactNode }
export interface TabsProps { label: string; tabs: Tab[]; initialTab?: string }

/** Base UI owns direction-aware roving focus; tabs select authorised views only. */
export function Tabs({ label, tabs, initialTab }: TabsProps) {
  const ids = new Set<string>();
  for (const tab of tabs) {
    if (!tab.id.trim() || ids.has(tab.id) || !tab.label.trim()) throw new TypeError("Tabs require unique IDs and readable labels");
    ids.add(tab.id);
  }
  if (initialTab !== undefined && !ids.has(initialTab)) throw new TypeError("Unknown initial tab");
  return <BaseTabs.Root defaultValue={initialTab ?? tabs[0]?.id}>
    <BaseTabs.List activateOnFocus aria-label={label} className={styles.list}>
      {tabs.map(tab => <BaseTabs.Tab key={tab.id} value={tab.id} className={styles.tab}>{tab.label}</BaseTabs.Tab>)}
    </BaseTabs.List>
    {tabs.map(tab => <BaseTabs.Panel key={tab.id} value={tab.id} className={styles.panel}>{tab.content}</BaseTabs.Panel>)}
  </BaseTabs.Root>;
}
