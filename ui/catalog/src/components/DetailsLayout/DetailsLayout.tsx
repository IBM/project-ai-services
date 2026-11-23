import type { ReactNode } from "react";
import {
  Grid,
  Column,
  SideNav,
  SideNavItems,
  SideNavLink,
} from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import { ArrowLeft } from "@carbon/icons-react";
import styles from "./DetailsLayout.module.scss";

export interface DetailsLayoutNavItem {
  key: string;
  label: string;
}

export interface DetailsLayoutProps {
  backLabel: string;
  onBack: () => void;
  title: ReactNode;
  titleAsText: string;
  subtitle?: ReactNode;
  navItems: DetailsLayoutNavItem[];
  activeKey: string;
  onNavChange?: (key: string) => void;
  children: ReactNode;
}

// Shared shell for detail screens: back breadcrumb, title, subtitle badges, side navigation and content area
const DetailsLayout = ({
  backLabel,
  onBack,
  title,
  titleAsText,
  subtitle,
  navItems,
  activeKey,
  onNavChange,
  children,
}: DetailsLayoutProps) => (
  <>
    <div>
      <PageHeader
        breadcrumbs={[
          {
            key: "back",
            href: "#",
            title: backLabel,
            label: (
              <span
                className={styles.breadcrumbLabel}
                onClick={(e) => {
                  e.preventDefault();
                  onBack();
                }}
              >
                <ArrowLeft size={16} />
                {backLabel}
              </span>
            ),
          },
        ]}
        breadcrumbOverflowAriaLabel="Show more breadcrumbs"
        title={{
          content: title,
          asText: titleAsText,
          breadcrumbContent: titleAsText,
        }}
        subtitle={<div className={styles.headerSubtitle}>{subtitle}</div>}
      />
      <hr className={styles.breadcrumbDivider} />
    </div>

    <Grid className={styles.mainGrid}>
      <Column sm={4} md={2} lg={3} className={styles.sidebarColumn}>
        <SideNav
          isFixedNav
          expanded
          aria-label="Side navigation"
          className={styles.sideNav}
        >
          <SideNavItems>
            {navItems.map((item) => (
              <SideNavLink
                key={item.key}
                isActive={activeKey === item.key}
                onClick={onNavChange && (() => onNavChange(item.key))}
              >
                {item.label}
              </SideNavLink>
            ))}
          </SideNavItems>
        </SideNav>
      </Column>

      <Column sm={4} md={6} lg={13} className={styles.contentColumn}>
        {children}
      </Column>
    </Grid>
  </>
);

export default DetailsLayout;
