---
title: Self-host your fonts
slug: self-host-your-fonts
date: 2026-05-03
publish_date: 2026-05-03
tags: webdev, performance, privacy
description: The shared cache argument for Google Fonts died in 2020 and the privacy argument was never great. Self-hosting with @fontsource is a one line change.
cover_image: self-host-your-fonts.webp
---

I self-host my [analytics](https://analytics.bythewood.me/), my [status page](https://status.bythewood.me/), and this blog, so it would be a bit strange to hand the fonts off to Google when they're the most-loaded asset on every page.

The original pitch for Google Fonts was that you get someone else's CDN for free and the file is probably already cached because every other site loads the same URL. That last part was the real selling point and it hasn't been true since Chrome 86 [partitioned the HTTP cache in October 2020](https://developer.chrome.com/blog/http-cache-partitioning) to deal with XS-Leak attacks, with Firefox and Safari doing the same. Two sites loading the same `fonts.gstatic.com` URL no longer share a cache entry since they're keyed by top-level site, so your visitors download the font fresh just like they would from your own server.

The privacy side is worse. The Munich Regional Court [ruled in January 2022](https://www.theregister.com/2022/01/31/website_fine_google_fonts_gdpr/) that embedding Google Fonts on a public site illegally transfers the visitor's IP address to Google in the United States and awarded the plaintiff €100 in damages, and the court noted that self-hosting is trivial so there's no good reason not to do it. A wave of warning letters followed across Germany. Your mileage will vary depending on where you are but the precedent is out there.

There are also bugs you won't know you have. Google [updates fonts in place](https://geoffgraham.me/google-fonts-can-update-at-any-time/) without telling anyone, so when Inter 4.0 shipped [the slanted italic quietly became a true italic](https://pimpmytype.com/google-fonts-hosting/) on every site pointing at the CDN. The subsetting has similar problems where the unicode-range CSS the API serves [silently omits characters](https://github.com/google/fonts/issues/4235) depending on which subset the browser asks for, like Fira Code's box-drawing glyphs, so things can render fine locally and come out as mojibake in production.

Fixing this is about a one line change. [`@fontsource`](https://fontsource.org/) packages 1500+ open source fonts as npm modules.

```bash
bun add @fontsource/inter
```

```js
import "@fontsource/inter";
```

The woff2 files end up in your own static directory behind your own cache headers and version locked to your `package.json`, so there's no third party DNS lookup, no IP address handed to Google, and nothing changes underneath you when someone else commits.
