// Compiles a station's timetable sheet to PDF in the browser, with the same
// Typst template and the same fonts the command line uses, so the file someone
// downloads here is the file `abfahrplan --pdf` produces.
//
// The compiler is a few tens of megabytes, so it is fetched on the first click
// and never again: most visitors want one sheet for one stop.

const here = new URL(".", import.meta.url); // …/static/
const root = new URL("../", here); // the site root

let booting = null;

function boot() {
  if (booting) return booting;
  booting = (async () => {
    const [{ $typst }, { loadFonts }] = await Promise.all([
      import("./typst/lib/typst/contrib/snippet.mjs"),
      import("./typst/lib/typst/options.init.mjs"),
    ]);

    $typst.setCompilerInitOptions({
      getModule: () => new URL("typst/lib/typst_ts_web_compiler_bg.wasm", here).href,
      beforeBuild: [
        loadFonts(
          ["Regular", "SemiBold", "Bold", "ExtraBold"].map(
            (weight) => new URL(`typst/fonts/FiraSans-${weight}.ttf`, here).href,
          ),
        ),
      ],
    });

    const template = await fetch(new URL("typst/timetable.typ", here)).then((r) => r.text());
    await $typst.addSource("/timetable.typ", template);
    return $typst;
  })();
  return booting;
}

// sheet returns one station's timetable as PDF bytes.
export async function sheet(slug) {
  const $typst = await boot();
  const data = await fetch(new URL(`s/${slug}.json`, root)).then((r) => {
    if (!r.ok) throw new Error(`no timetable for ${slug}`);
    return r.text();
  });
  // The template reads timetable.json from beside itself; both live at the
  // root of the compiler's in-memory filesystem.
  await $typst.addSource("/timetable.json", data);
  return $typst.pdf({ mainFilePath: "/timetable.typ" });
}

// download compiles a sheet and hands it to the browser as a file.
export async function download(slug, name) {
  const pdf = await sheet(slug);
  const url = URL.createObjectURL(new Blob([pdf], { type: "application/pdf" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = `${name || slug}.pdf`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  // Revoke late: Firefox cancels an in-flight download if the URL goes early.
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

// wire turns every [data-sheet] element into a button that compiles on click.
// Progressive enhancement: without JavaScript nothing here runs, which is why
// each station page also shows its departures as a table.
export function wire(scope = document) {
  for (const button of scope.querySelectorAll("[data-sheet]")) {
    if (button.dataset.wired) continue;
    button.dataset.wired = "1";
    button.addEventListener("click", async (event) => {
      event.preventDefault();
      const label = button.textContent;
      button.textContent = "wird erzeugt…";
      button.setAttribute("aria-busy", "true");
      try {
        await download(button.dataset.sheet, button.dataset.name);
        button.textContent = label;
      } catch (error) {
        console.error(error);
        button.textContent = "Fehler beim Erzeugen";
        setTimeout(() => { button.textContent = label; }, 4000);
      } finally {
        button.removeAttribute("aria-busy");
      }
    });
  }
}
