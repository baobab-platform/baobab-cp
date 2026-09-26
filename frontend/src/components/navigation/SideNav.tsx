import styles from "./SideNav.module.css";

export interface NavItem {
  href: string;
  label: string;
}

export interface SideNavProps {
  /** Distinguishes this landmark from other navigation (WCAG 1.3.1). */
  label: string;
  items: NavItem[];
  /** The href of the current page, marked with aria-current. */
  current?: string;
}

/**
 * Primary navigation. Which items appear is decided from the principal's
 * effective authority as the Control Plane reports it (FE-04); hiding an
 * item is never the security control.
 */
export function SideNav({ label, items, current }: SideNavProps) {
  return (
    <nav aria-label={label} className={styles.nav}>
      <ul className={styles.list}>
        {items.map((item) => (
          <li key={item.href}>
            <a className={styles.link} href={item.href} aria-current={item.href === current ? "page" : undefined}>
              {item.label}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}
