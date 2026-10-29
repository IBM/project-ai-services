import { useEffect } from "react";
import { useLocation } from "react-router";
import { APP_NAME, PAGE_TITLES, ROUTES } from "@/constants";
import type { PageKey } from "@/constants";

// Sets the browser tab title to "<app name> - <page name>" based on the current route
export const useDocumentTitle = () => {
  const { pathname } = useLocation();

  useEffect(() => {
    const pageKey = (Object.keys(PAGE_TITLES) as PageKey[]).find(
      (key) =>
        pathname === ROUTES[key] || pathname.startsWith(`${ROUTES[key]}/`),
    );

    document.title = pageKey
      ? `${APP_NAME} - ${PAGE_TITLES[pageKey]}`
      : APP_NAME;

    return () => {
      document.title = APP_NAME;
    };
  }, [pathname]);
};
