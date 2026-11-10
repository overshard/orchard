// The stylesheet is an entry of its own. Imported from each page's script it
// would land in a shared chunk, and the manifest reader only lists the
// stylesheets on the entry a page names.
import "@fontsource/space-grotesk/500.css";
import "@fontsource/space-grotesk/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "./index.css";
