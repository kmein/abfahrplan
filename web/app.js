// maplibre-gl v6 ships ESM with named exports only -- there is no default
import * as maplibregl from "./maplibre-gl.mjs";

// pmtiles.js is loaded as a classic script and registers the global; the
// protocol has to exist before any style references a pmtiles:// url
maplibregl.addProtocol("pmtiles", new pmtiles.Protocol().tile);

// BVG's own colours for bus, U-Bahn and tram, sampled from their print PDFs;
// S-Bahn green is the S-Bahn's. Regional rail and ferry are neutral -- they are
// rare inside Berlin and not BVG's to colour.
const MODE_COLOUR = {
  sbahn: "#008D4F",
  ubahn: "#1565AF",
  tram: "#ED1C24",
  bus: "#A0148E",
  rail: "#555150",
  ferry: "#0B7285",
};

// The basemap is deliberately recessive: it exists so people can tell which
// dot is their stop, not to be looked at. Protomaps v4 layer names.
const basemapLayers = [
  { id: "earth", type: "background", paint: { "background-color": "#F6F5F2" } },
  { id: "landcover", type: "fill", source: "basemap", "source-layer": "landcover",
    paint: { "fill-color": "#EDEFE8", "fill-opacity": 0.7 } },
  { id: "landuse", type: "fill", source: "basemap", "source-layer": "landuse",
    paint: { "fill-color": "#EEEDE9" } },
  { id: "water", type: "fill", source: "basemap", "source-layer": "water",
    paint: { "fill-color": "#CBDCEA" } },
  { id: "buildings", type: "fill", source: "basemap", "source-layer": "buildings",
    minzoom: 14, paint: { "fill-color": "#E4E2DE" } },
  { id: "roads-minor", type: "line", source: "basemap", "source-layer": "roads",
    minzoom: 12, filter: ["!in", "kind", "highway", "major_road"],
    paint: { "line-color": "#FFFFFF", "line-width": ["interpolate", ["linear"], ["zoom"], 12, 0.5, 17, 4] } },
  { id: "roads-major", type: "line", source: "basemap", "source-layer": "roads",
    filter: ["in", "kind", "highway", "major_road"],
    paint: { "line-color": "#FFFFFF", "line-width": ["interpolate", ["linear"], ["zoom"], 8, 0.6, 17, 7] } },
  { id: "boundaries", type: "line", source: "basemap", "source-layer": "boundaries",
    paint: { "line-color": "#BDBAB8", "line-dasharray": [3, 2], "line-width": 1 } },
  { id: "places", type: "symbol", source: "basemap", "source-layer": "places",
    filter: ["in", "kind", "city", "town", "village", "neighbourhood", "suburb"],
    layout: {
      "text-field": ["get", "name"],
      "text-font": ["Noto Sans Regular"],
      "text-size": ["interpolate", ["linear"], ["zoom"], 9, 10, 15, 14],
    },
    paint: { "text-color": "#7A7775", "text-halo-color": "#F6F5F2", "text-halo-width": 1.4 } },
];

// A station wears the colour of the most distinctive mode that calls there, so
// an S-Bahn stop with a bus outside it still reads as an S-Bahn stop.
const colourByMode = [
  "match", ["get", "kind"],
  "sbahn", MODE_COLOUR.sbahn,
  "ubahn", MODE_COLOUR.ubahn,
  "tram", MODE_COLOUR.tram,
  "rail", MODE_COLOUR.rail,
  "ferry", MODE_COLOUR.ferry,
  MODE_COLOUR.bus,
];

let map = null;

function startMap() {
  map = new maplibregl.Map({
    container: "map",
    hash: true,
    attributionControl: { compact: true },
    style: {
      version: 8,
      glyphs: "static/glyphs/{fontstack}/{range}.pbf",
      sources: {
        basemap: {
          type: "vector",
          url: "pmtiles://basemap.pmtiles",
          attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>, Fahrplandaten VBB',
        },
      },
      layers: basemapLayers,
    },
    center: [13.404, 52.52],
    zoom: 11,
  });
  map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");
  map.on("load", addStations);
}

// searching should not require getting the umlauts right
const fold = (s) => s.toLowerCase()
  .replaceAll("ä", "ae").replaceAll("ö", "oe").replaceAll("ü", "ue").replaceAll("ß", "ss")
  .replace(/[^a-z0-9]+/g, " ").trim();

let stations = [];
const results = document.getElementById("results");
const count = document.getElementById("count");
const query = document.getElementById("q");

const badges = (station) => station.routes.slice(0, 8).map((route, i) => {
  const kind = (station.kinds && station.kinds[i]) || "bus";
  return `<span class="badge ${kind}">${route}</span>`;
}).join("");

function show(list) {
  count.textContent = list.length === stations.length
    ? `${stations.length.toLocaleString("de")} Haltestellen`
    : `${list.length.toLocaleString("de")} Treffer`;
  results.innerHTML = list.slice(0, 200).map((station) => `
    <button class="hit" data-slug="${station.slug}">
      <strong>${station.name}</strong>
      <span class="lines">${badges(station)}</span>
    </button>`).join("");
}

function select(slug) {
  const station = stations.find((s) => s.slug === slug);
  if (!station) return;
  if (!map) {
    window.location.href = `s/${station.slug}.html`;
    return;
  }
  map.flyTo({ center: [station.lon, station.lat], zoom: 15 });
  new maplibregl.Popup({ closeButton: true })
    .setLngLat([station.lon, station.lat])
    .setHTML(`
      <div class="popup-name">${station.name}</div>
      <div>${badges(station)}</div>
      <a class="sheet" href="s/${station.slug}.pdf">Fahrplan als PDF</a>
      <div style="margin-top:6px"><a href="s/${station.slug}.html">Abfahrten ansehen</a></div>`)
    .addTo(map);
}

results.addEventListener("click", (event) => {
  const hit = event.target.closest(".hit");
  if (hit) select(hit.dataset.slug);
});

query.addEventListener("input", () => {
  const needle = fold(query.value);
  show(needle === "" ? stations : stations.filter((s) => s.folded.includes(needle)));
});

fetch("meta.json").then((r) => r.json()).then((meta) => {
  document.getElementById("validity").innerHTML =
    `gültig ${meta.valid_from} bis ${meta.valid_to}<br>${meta.stations.toLocaleString("de")} Haltestellen`;
});

// The list and the search box do not need the map, so build them first: a
// browser with WebGL turned off still gets a usable page rather than a blank
// one, and a failure in MapLibre cannot take the whole script down with it.
fetch("stations.json").then((r) => r.json()).then((loaded) => {
  stations = loaded.map((s) => ({ ...s, folded: fold(s.name) }));
  show(stations);
  try {
    startMap();
  } catch (error) {
    console.error("map unavailable:", error);
    document.getElementById("map").innerHTML =
      '<p style="padding:20px;color:#605D5E">Karte nicht verfügbar. Die Suche funktioniert trotzdem.</p>';
  }
});

function addStations() {
  map.addSource("stations", {
    type: "geojson",
    data: {
      type: "FeatureCollection",
      features: stations.map((s) => ({
        type: "Feature",
        geometry: { type: "Point", coordinates: [s.lon, s.lat] },
        properties: { name: s.name, slug: s.slug, kind: s.kind || "bus" },
      })),
    },
  });

  // No clustering: a few thousand points is nothing for MapLibre, and the
  // unclustered dots draw the shape of the network, which a cluster bubble
  // hides.
  map.addLayer({
    id: "stations",
    type: "circle",
    source: "stations",
    paint: {
      "circle-color": colourByMode,
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 10, 1.8, 12, 3, 14, 4.5, 17, 8],
      "circle-stroke-width": ["interpolate", ["linear"], ["zoom"], 11, 0.4, 14, 1.4],
      "circle-stroke-color": "#FDFDFC",
      "circle-opacity": ["interpolate", ["linear"], ["zoom"], 10, 0.75, 13, 1],
    },
  });

  map.addLayer({
    id: "station-labels",
    type: "symbol",
    source: "stations",
    minzoom: 14,
    layout: {
      "text-field": ["get", "name"],
      "text-font": ["Noto Sans Regular"],
      "text-size": 11,
      "text-offset": [0, 1.1],
      "text-anchor": "top",
      "text-optional": true,
    },
    paint: { "text-color": "#242021", "text-halo-color": "#FDFDFC", "text-halo-width": 1.6 },
  });

  map.on("click", "stations", (event) => select(event.features[0].properties.slug));
  map.on("mouseenter", "stations", () => { map.getCanvas().style.cursor = "pointer"; });
  map.on("mouseleave", "stations", () => { map.getCanvas().style.cursor = ""; });
}
