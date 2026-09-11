# A VM that runs the module for real. Evaluating it only proves the Nix is
# well-formed; this proves the unit starts under its sandbox, that Typst can
# fork its workers inside it, that nginx can read what StateDirectory creates,
# and that the fonts and MIME types the map depends on survive packaging.
{ pkgs }:

pkgs.testers.runNixOSTest {
  name = "abfahrplan";

  nodes.machine =
    { lib, pkgs, ... }:
    {
      imports = [ ./module.nix ];

      services.abfahrplan = {
        enable = true;
        # runNixOSTest pins nixpkgs read-only, so hand the package over
        # directly rather than through the overlay.
        package = pkgs.callPackage ./package.nix { };
        domain = "abfahrplan.test";
        # The fixture stands in for the feed: two hours of buses and a tram,
        # valid until 2099 so the expired-feed guard never fires and this test
        # does not rot.
        feedUrl = null;
        feedFile = ../testdata/fixture-gtfs.zip;
        bbox = null;
        trim = "(Berlin)";
        basemap.enable = false;
        jobs = 2;
      };

      # No ACME in a VM.
      services.nginx.virtualHosts."abfahrplan.test" = {
        forceSSL = lib.mkForce false;
        enableACME = lib.mkForce false;
      };

      networking.hosts."127.0.0.1" = [ "abfahrplan.test" ];
      environment.systemPackages = [ pkgs.poppler-utils ];
      virtualisation.memorySize = 2048;
    };

  testScript = ''
    machine.wait_for_unit("multi-user.target")
    machine.wait_for_unit("nginx.service")

    # The generator is wantedBy multi-user.target, so a fresh boot populates
    # itself without waiting for the timer. It is a oneshot: by the time we
    # look it has finished and gone inactive, so wait for what it produced and
    # check how it exited rather than for it to be running.
    machine.wait_until_succeeds("test -e /var/lib/abfahrplan/current/index.html", timeout=180)
    machine.succeed("systemctl show -p Result abfahrplan-generate.service | grep -q '=success'")
    machine.succeed("test -L /var/lib/abfahrplan/current")

    with subtest("the site is served"):
        machine.succeed("curl -sSf http://abfahrplan.test/ | grep -q Abfahrplan")
        machine.succeed("curl -sSf http://abfahrplan.test/stations.json | grep -q musterplatz")
        machine.succeed("curl -sSf http://abfahrplan.test/robots.txt | grep -q Disallow")
        machine.succeed("curl -sSf http://abfahrplan.test/favicon.svg | grep -q svg")

    with subtest("the credit the licence asks for reaches the pages"):
        machine.succeed(
            "curl -sSf http://abfahrplan.test/ | grep -q 'VBB Verkehrsverbund Berlin-Brandenburg GmbH'"
        )

    with subtest("ES modules are served as javascript"):
        # nginx's mime.types predates .mjs; without the override the browser
        # refuses maplibre and the map silently stays blank.
        kind = machine.succeed(
            "curl -sSfI http://abfahrplan.test/static/maplibre-gl.mjs | tr -d '\\r' | grep -i '^content-type:'"
        )
        assert "javascript" in kind, f"maplibre-gl.mjs served as {kind!r}"

    with subtest("a station sheet renders, in the fonts we pinned"):
        machine.succeed("curl -sSf -o /tmp/sheet.pdf http://abfahrplan.test/s/musterplatz.pdf")
        machine.succeed("head -c 4 /tmp/sheet.pdf | grep -q '%PDF'")
        # typst substitutes a missing font silently, so assert the face by name
        # rather than trusting that it rendered at all.
        fonts = machine.succeed("pdffonts /tmp/sheet.pdf")
        assert "FiraSans" in fonts, f"sheet was not set in Fira Sans:\n{fonts}"

    with subtest("a station page carries its departures"):
        page = machine.succeed("curl -sSf http://abfahrplan.test/s/musterplatz.html")
        assert "Musterplatz" in page
        assert "42" in page, "the bus line is missing from the page"

    with subtest("a rerun with an unchanged feed republishes rather than failing"):
        machine.succeed("systemctl start abfahrplan-generate.service")
        machine.succeed("test -e /var/lib/abfahrplan/current/index.html")
  '';
}
