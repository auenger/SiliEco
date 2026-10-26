import { LocaleProvider } from "@/features/landing/i18n";
import { getRequestLocale } from "@/lib/request-locale";

const jsonLd = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "Organization",
      name: "Silieco",
      url: "https://www.silieco.ai",
      parentOrganization: {
        "@type": "Organization",
        name: "SiliRoot",
        url: "https://siliroot.com/",
      },
      email: "machi@siliroot.com",
    },
    {
      "@type": "SoftwareApplication",
      name: "Silieco",
      applicationCategory: "ProjectManagement",
      operatingSystem: "Web",
      description:
        "Open-source workspace for human and agent collaboration through tasks, SOPs, and evidence-based handoffs.",
      offers: {
        "@type": "Offer",
        price: "0",
        priceCurrency: "USD",
      },
    },
  ],
};

export default async function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const initialLocale = await getRequestLocale();

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />
      <div className="landing-light h-full overflow-x-hidden overflow-y-auto bg-white">
        <LocaleProvider initialLocale={initialLocale}>
          {children}
        </LocaleProvider>
      </div>
    </>
  );
}
