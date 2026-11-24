import type { ReactNode, ComponentProps } from "react";
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
  pageActions?: ComponentProps<typeof PageHeader>["pageActions"];
  pageActionsOverflowLabel?: string;
  fullWidthGrid?: ComponentProps<typeof PageHeader>["fullWidthGrid"];
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
  pageActions,
  pageActionsOverflowLabel,
  fullWidthGrid,
  navItems,
  activeKey,
  onNavChange,
  children,
}: DetailsLayoutProps) => {
  const hasPageActions = Array.isArray(pageActions)
    ? pageActions.length > 0
    : Boolean(pageActions);

  const pageActionsProps =
    pageActions && hasPageActions
      ? {
          pageActions,
          pageActionsOverflowLabel: pageActionsOverflowLabel ?? "More actions",
        }
      : {};

  return (
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
          {...pageActionsProps}
          fullWidthGrid={fullWidthGrid}
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
};

export default DetailsLayout;
