import type { Metadata } from "next";
import { SiliecoLanding } from "@/features/landing/components/silieco-landing";
import { RedirectIfAuthenticated } from "@/features/landing/components/redirect-if-authenticated";

export const metadata: Metadata = {
  title: {
    absolute: "Silieco — Humans & Agents, Working Together",
  },
  description:
    "An open-source workspace where humans and agents collaborate through shared tasks, SOPs, and clear handoffs.",
  openGraph: {
    title: "Silieco — Humans & Agents, Working Together",
    description:
      "Define goals, coordinate agent execution, and review outcomes together.",
    url: "/",
  },
  alternates: {
    canonical: "/",
  },
};

export default function LandingPage() {
  return (
    <>
      <RedirectIfAuthenticated />
      <SiliecoLanding />
    </>
  );
}
