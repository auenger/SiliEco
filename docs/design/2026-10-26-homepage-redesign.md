# Silieco single-page website

The current Tuling `src/pages/index.astro`, `src/styles/origin.css`, and `src/layouts/OriginLayout.astro` define the visual reference: Songti display typography, vermilion accents, fine separators, flat surfaces, and a page-corner theme switch. Both light and dark themes are available; every fresh visit defaults to light. The fold uses the SiliRoot hover, focus, hint, and click treatment.

## Homepage and navigation

Only the marketing root page remains. The old about, contact-sales, changelog, use-case, download and homepage-alias route files were removed. Their old URLs return permanent redirects to `/`. The previous static `public/product-home` homepage and its root rewrite were removed so `/` serves the new homepage. The sitemap contains only `/`.

There is no FAQ, footer logo, giant wordmark, GitHub link, docs link, case-page link, login CTA, or sales-page link on the homepage. Navigation stays within the page. The only outgoing destinations are `mailto:machi@siliroot.com` and `https://siliroot.com/`. Existing product workspace routes and backend APIs are outside this marketing-page change.

## Ecosystem source

`ecosystem-scene.tsx` adapts `promo/src/ecosystem-ending.tsx`, retaining the four SOPs, portraits, task flow, hub connections, and timing. Original promotional source is unchanged. Assets live under `public/media/ecosystem/`. Remotion Player and Remotion are pinned to 4.0.538, matching the original project.

The web scene starts at the established ecosystem state, six seconds into the original ending. Cinematic titles and repository text are removed. Surfaces and labels follow the selected theme. Desktop uses the original four-corner composition; phone/tablet stack the same SOPs with larger labels. The source scene remains Chinese, matching the original film; the surrounding homepage retains four languages.

The scene has a pause control and pauses when hidden or offscreen. Reduced motion keeps a complete static scene. The 88-second loop aligns the original 4-second connector and 5.5-second workflow cycles. The complete original 76-second MP4 remains in a separate section with native playback controls and subtitles.

## Validation

Browser checks cover light/dark switching, the corner-fold control, portrait loading, responsive layout, animated frame progression, pause, mobile navigation, and links. The removed marketing URLs return HTTP 308 to `/`. No FAQ or footer logo is present, and outgoing homepage anchors are limited to the requested email and parent website.

Preview: http://localhost:3100/

The local API backend is not running, so authenticated product flows are not part of the preview verification. No deployment was performed.
