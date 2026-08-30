// Three faces, three jobs.
//
//   Newsreader        the wordmark and headings. A serif is the single fastest
//                     way for this not to read as another forge: GitHub, GitLab,
//                     Gitea and Forgejo are all sans-and-mono. It also suits what
//                     this actually is, which is an archive rather than an
//                     industrial tool.
//   IBM Plex Sans     UI and prose. Setting the whole page in mono, as the first
//                     draft did, made READMEs hard to read.
//   Monaspace Argon   anything literally code: paths, SHAs, refs, commands, diffs.
import "@fontsource/newsreader/400.css";
import "@fontsource/newsreader/600.css";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/monaspace-argon/400.css";
import "./styles/base.scss";

import "./scripts/refswitch.js";
