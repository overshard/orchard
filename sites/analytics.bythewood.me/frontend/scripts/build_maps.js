// Builds the map data from Natural Earth, via martynafford/natural-earth-geojson.
// Writes build/static_maps/world.json keyed by ISO_A2, plus one
// build/static_maps/admin1/{ISO_A2}.json per country. `bun run build:maps`.

import { mkdir, writeFile } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { topology } from "topojson-server";

const __dirname = dirname(fileURLToPath(import.meta.url));
const OUT_DIR = resolve(__dirname, "../../build/static_maps");

// 110m is plenty for the world view. admin-1 has to be 10m, since it is the
// only Natural Earth tier with full per-country coverage.
const BASE = "https://raw.githubusercontent.com/martynafford/natural-earth-geojson/master";
const ADMIN0_URL = `${BASE}/110m/cultural/ne_110m_admin_0_countries.json`;
const ADMIN1_URL = `${BASE}/10m/cultural/ne_10m_admin_1_states_provinces.json`;

// 1e5 keeps coastlines smooth at screen sizes and still cuts the file size a
// long way against raw GeoJSON. d3-geo handles the dequantization.
const QUANTIZATION = 1e5;

async function fetchJson(url) {
  process.stdout.write(`  GET ${url}\n`);
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText} - ${url}`);
  return res.json();
}

// Natural Earth has a long-running bug where France and Norway get
// ISO_A2 = "-99", from an EU/Schengen dispute baked into the source. They are
// the only two, so map them back from ISO_A3.
const A3_TO_A2_OVERRIDES = {
  FRA: "FR",
  NOR: "NO",
};

function normalizeCountryCode(props) {
  const code = props.ISO_A2 ?? props.iso_a2;
  if (code && code !== "-99" && code !== -99) return code;
  const eh = props.ISO_A2_EH ?? props.iso_a2_eh;
  if (eh && eh !== "-99" && eh !== -99) return eh;
  // ISO_A3 is also "-99" for the same disputed records, so fall back to
  // ADM0_A3 which Natural Earth always populates.
  const a3 = props.ADM0_A3 ?? props.adm0_a3 ?? props.ISO_A3 ?? props.iso_a3;
  if (a3 && a3 !== "-99" && A3_TO_A2_OVERRIDES[a3]) return A3_TO_A2_OVERRIDES[a3];
  return null;
}

function trimCountryProps(feature) {
  // The client only needs a name and an ISO code, so the other Natural Earth
  // fields are dropped to keep the payload down.
  const p = feature.properties || {};
  const iso = normalizeCountryCode(p);
  return {
    ...feature,
    id: iso,
    properties: {
      iso: iso,
      name: p.NAME ?? p.ADMIN ?? p.name ?? "",
    },
  };
}

function trimAdmin1Props(feature) {
  const p = feature.properties || {};
  // Natural Earth's `name` is the local form ("Bayern") and DB-IP returns the
  // English one ("Bavaria"), so both are kept and the lookup can match either.
  return {
    ...feature,
    properties: {
      iso_3166_2: p.iso_3166_2 ?? "",
      postal: p.postal ?? "",
      name: p.name ?? "",
      name_alt: p.name_alt ?? "",
    },
  };
}

async function buildWorld(admin0) {
  const features = admin0.features
    .map(trimCountryProps)
    .filter((f) => f.id);
  const topo = topology({ countries: { type: "FeatureCollection", features } }, QUANTIZATION);
  const path = resolve(OUT_DIR, "world.json");
  await writeFile(path, JSON.stringify(topo));
  return { path, count: features.length, bytes: JSON.stringify(topo).length };
}

async function buildAdmin1(admin1) {
  const byCountry = new Map();
  for (const f of admin1.features) {
    const iso = normalizeCountryCode(f.properties || {});
    if (!iso) continue;
    if (!byCountry.has(iso)) byCountry.set(iso, []);
    byCountry.get(iso).push(trimAdmin1Props(f));
  }

  await mkdir(resolve(OUT_DIR, "admin1"), { recursive: true });

  let totalBytes = 0;
  for (const [iso, features] of byCountry) {
    const topo = topology({ regions: { type: "FeatureCollection", features } }, QUANTIZATION);
    const json = JSON.stringify(topo);
    await writeFile(resolve(OUT_DIR, "admin1", `${iso}.json`), json);
    totalBytes += json.length;
  }
  return { count: byCountry.size, bytes: totalBytes };
}

async function main() {
  await mkdir(OUT_DIR, { recursive: true });

  console.log("Downloading Natural Earth source data...");
  const [admin0, admin1] = await Promise.all([
    fetchJson(ADMIN0_URL),
    fetchJson(ADMIN1_URL),
  ]);

  console.log("Building world topology...");
  const world = await buildWorld(admin0);
  console.log(`  ${world.count} countries -> ${world.path} (${(world.bytes / 1024).toFixed(1)} KB)`);

  console.log("Building per-country admin-1 topologies...");
  const a1 = await buildAdmin1(admin1);
  console.log(`  ${a1.count} country files (${(a1.bytes / 1024).toFixed(1)} KB total)`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
