import { starfield } from "./scripts/starfield.js";
import { copyButtons } from "./scripts/copy.js";

import "./styles/pages.scss";

const stars = document.querySelector("[data-starfield]");
if (stars) starfield(stars);

copyButtons();
