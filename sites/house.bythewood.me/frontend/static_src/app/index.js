// Fraunces for headings and Spectral for reading and for every figure. Both self
// hosted through the bundler, never a font CDN.
//
// Fraunces ships one set of proportional digits and no tabular set at all, so a
// column of money set in it visibly jitters, and there is no CSS that fixes that.
// Every number on this site is therefore Spectral.
import "@fontsource/fraunces/400.css";
import "@fontsource/fraunces/500.css";
import "@fontsource/fraunces/600.css";
import "@fontsource/fraunces/700.css";
import "@fontsource/spectral/400.css";
import "@fontsource/spectral/500.css";
import "@fontsource/spectral/600.css";
import "@fontsource/spectral/700.css";
import "@fontsource/spectral/800.css";

import "./styles/app.scss";

import "./scripts/photos.js";
import "./scripts/verdict.js";
import "./scripts/working.js";
import "./scripts/check.js";
