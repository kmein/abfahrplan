{
  lib,
  buildGoModule,
  makeWrapper,
  typst,
  fira-sans,
}:

buildGoModule {
  pname = "abfahrplan";
  version = "0.1.0";

  src = lib.cleanSource ../.;

  vendorHash = "sha256-B681CQjT650TgE4Kid1u2+KqYRV0uBtSefVIhSJ0ukI=";

  nativeBuildInputs = [ makeWrapper ];

  # The Typst template and the web assets travel inside the binaries via
  # go:embed, so only the tools they shell out to need wiring up. Typst
  # substitutes a missing font silently rather than failing, so pin the font
  # path too instead of trusting whatever the host happens to have installed.
  postInstall = ''
    for binary in $out/bin/*; do
      wrapProgram "$binary" \
        --set ABFAHRPLAN_TYPST ${lib.getExe typst} \
        --set ABFAHRPLAN_FONT_PATH ${fira-sans}/share/fonts
    done
  '';

  meta = {
    description = "Compact BVG-style departure timetables from a GTFS feed, as PDFs and a static site";
    homepage = "https://github.com/kmein/abfahrplan";
    license = lib.licenses.agpl3Only;
    mainProgram = "abfahrplan";
  };
}
