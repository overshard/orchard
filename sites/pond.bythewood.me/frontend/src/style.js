// The stylesheet is an entry of its own. Imported from each page's script it
// would land in a shared chunk, and the manifest reader only lists the
// stylesheets on the entry a page names.
import "@fontsource/pixelify-sans/500.css";
import "@fontsource/pixelify-sans/700.css";
import "@fontsource/inter/400.css";
import "@fontsource/inter/400-italic.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/inter/700.css";
import "./index.css";
