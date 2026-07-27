import { AppShell } from "@/components/AppShell";
import { AuthGate } from "@/components/AuthGate";

export default function ShellLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <AuthGate>
      <AppShell>{children}</AppShell>
    </AuthGate>
  );
}
