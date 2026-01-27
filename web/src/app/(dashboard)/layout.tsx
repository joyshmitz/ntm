"use client";

/**
 * Dashboard Layout
 *
 * Wraps authenticated pages with:
 * - TanStack Query provider
 * - WebSocket connection
 * - Navigation
 * - Connection status indicator
 */

import { useEffect, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { getConnectionConfig } from "@/lib/api/client";
import { NtmQueryProvider, useConnection } from "@/lib/hooks/use-query";
import { NavBar } from "@/components/layout/nav-bar";

interface DashboardLayoutProps {
  children: ReactNode;
}

function DashboardContent({ children }: DashboardLayoutProps) {
  const { wsState, isConnected } = useConnection();

  return (
    <div className="min-h-screen flex flex-col bg-gray-50 dark:bg-gray-900">
      <NavBar wsState={wsState} />
      <main className="flex-1 p-4 sm:p-6 lg:p-8">
        {!isConnected && wsState === "reconnecting" && (
          <div className="mb-4 p-3 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md">
            <p className="text-sm text-yellow-700 dark:text-yellow-400">
              Reconnecting to server...
            </p>
          </div>
        )}
        {children}
      </main>
    </div>
  );
}

export default function DashboardLayout({ children }: DashboardLayoutProps) {
  const router = useRouter();
  const [isChecking, setIsChecking] = useState(true);

  useEffect(() => {
    // Check if we have connection config
    const config = getConnectionConfig();
    if (!config) {
      router.replace("/connect");
      return;
    }
    setIsChecking(false);
  }, [router]);

  if (isChecking) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="animate-spin h-8 w-8 border-4 border-blue-500 border-t-transparent rounded-full" />
      </div>
    );
  }

  return (
    <NtmQueryProvider>
      <DashboardContent>{children}</DashboardContent>
    </NtmQueryProvider>
  );
}
