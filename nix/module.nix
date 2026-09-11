{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.abfahrplan;
  inherit (lib)
    mkEnableOption
    mkOption
    mkIf
    types
    optionals
    optionalString
    ;
in
{
  options.services.abfahrplan = {
    enable = mkEnableOption "the abfahrplan timetable site";

    package = mkOption {
      type = types.package;
      default = pkgs.abfahrplan;
      defaultText = lib.literalExpression "pkgs.abfahrplan";
      description = "The abfahrplan package to use.";
    };

    stateDir = mkOption {
      type = types.path;
      default = "/var/lib/abfahrplan";
      description = ''
        Where builds are generated. The site is served out of
        `''${stateDir}/current`, a symlink swapped atomically when a build
        finishes, so a rebuild never interrupts serving.
      '';
    };

    feedUrl = mkOption {
      type = types.str;
      default = "https://unternehmen.vbb.de/fileadmin/user_upload/VBB/Dokumente/API-Datensaetze/gtfs-mastscharf/GTFS.zip";
      description = ''
        Where to fetch the GTFS feed. Fetched conditionally: if the server says
        it has not changed, the build is skipped entirely.
      '';
    };

    bbox = mkOption {
      type = types.nullOr types.str;
      default = "13.0883,52.3383,13.7612,52.6755";
      example = "11.2,51.3,14.8,53.6";
      description = ''
        Only generate stations inside `minLon,minLat,maxLon,maxLat`. The
        default is Berlin. The map fences itself to the same box, so widen this
        and the basemap together or the extra stations sit on blank tiles.
      '';
    };

    basemap = {
      enable = mkOption {
        type = types.bool;
        default = true;
        description = ''
          Cut a PMTiles basemap from the Protomaps daily planet and keep it
          fresh, covering the same area as {option}`bbox`. Berlin at zoom 14
          comes to about 30 MB and takes a quarter of a minute.

          Turn this off to bring your own file at {option}`basemap.path`, or to
          run without one: the map then draws stations on a blank ground, which
          reads well for a region and poorly for a city.
        '';
      };

      path = mkOption {
        type = types.path;
        default = "${cfg.stateDir}/basemap.pmtiles";
        defaultText = lib.literalExpression ''"''${config.services.abfahrplan.stateDir}/basemap.pmtiles"'';
        description = "Where the basemap lives. Published into each build and served by byte range.";
      };

      maxZoom = mkOption {
        type = types.int;
        default = 14;
        description = "Deepest zoom to cut. Each level beyond this multiplies the size.";
      };

      refreshSchedule = mkOption {
        type = types.str;
        default = "monthly";
        description = ''
          How often to re-cut the basemap, as a systemd `OnCalendar`
          expression. Streets move slowly; monthly is generous.
        '';
      };
    };

    trim = mkOption {
      type = types.str;
      default = "(Berlin)";
      description = ''
        Removed from station names and headsigns wherever it occurs. VBB tags
        every Berlin stop this way, which says nothing in a Berlin-only site.
      '';
    };

    refreshSchedule = mkOption {
      type = types.str;
      default = "daily";
      description = ''
        How often to check the feed, as a systemd `OnCalendar` expression. The
        check is one conditional request; a build only happens when the feed
        has actually changed, which for VBB is every few weeks.
      '';
    };

    jobs = mkOption {
      type = types.nullOr types.int;
      default = null;
      description = "How many timetables to render at once. Defaults to one per core.";
    };

    keep = mkOption {
      type = types.int;
      default = 2;
      description = "How many previous builds to keep before pruning.";
    };

    memoryMax = mkOption {
      type = types.str;
      default = "3G";
      description = ''
        Memory ceiling for the generator. Parsing the VBB feed peaks around
        700 MB and collecting every station adds a few hundred more; the
        ceiling is there so a larger feed fails loudly rather than taking the
        machine down with it.
      '';
    };

    domain = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "abfahrplan.example.org";
      description = ''
        Serve the site from this nginx virtual host, with ACME. Null configures
        no web server, leaving {option}`stateDir` for you to point one at.
      '';
    };

    user = mkOption {
      type = types.str;
      default = "abfahrplan";
      description = "User the generator runs as.";
    };

    group = mkOption {
      type = types.str;
      default = "abfahrplan";
      description = "Group the generator runs as.";
    };
  };

  config = mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.stateDir;
    };
    users.groups.${cfg.group} = { };

    systemd.services.abfahrplan-generate = {
      description = "Generate the abfahrplan timetable site";
      after = [
        "network-online.target"
      ]
      ++ optionals cfg.basemap.enable [ "abfahrplan-basemap.service" ];
      wants = [ "network-online.target" ];
      # Populate a fresh machine without waiting for the first timer tick.
      wantedBy = [ "multi-user.target" ];

      serviceConfig = {
        Type = "oneshot";
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = baseNameOf cfg.stateDir;
        WorkingDirectory = cfg.stateDir;

        ExecStart = lib.escapeShellArgs (
          [
            "${cfg.package}/bin/abfahrplan-generate"
            "--url"
            cfg.feedUrl
            "--gtfs"
            "${cfg.stateDir}/GTFS.zip"
            "--out"
            cfg.stateDir
            "--trim"
            cfg.trim
            "--keep"
            (toString cfg.keep)
          ]
          ++ optionals (cfg.bbox != null) [
            "--bbox"
            cfg.bbox
          ]
          ++ [
            "--basemap"
            (toString cfg.basemap.path)
          ]
          ++ optionals (cfg.jobs != null) [
            "--jobs"
            (toString cfg.jobs)
          ]
        );

        # A batch job that touches one directory and one URL.
        MemoryMax = cfg.memoryMax;
        Nice = 19;
        IOSchedulingClass = "idle";
        ReadWritePaths = [ cfg.stateDir ];
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictAddressFamilies = [
          "AF_INET"
          "AF_INET6"
          "AF_UNIX"
        ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        NoNewPrivileges = true;
        CapabilityBoundingSet = [ "" ];
        SystemCallFilter = [ "@system-service" ];
        SystemCallArchitectures = "native";
        UMask = "0022"; # the web server has to read what this writes
      };
    };

    # Cutting the basemap is its own job: it fails independently, on its own
    # schedule, and a failure leaves the previous file and the site alone.
    systemd.services.abfahrplan-basemap = mkIf cfg.basemap.enable {
      description = "Cut a PMTiles basemap for abfahrplan";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      before = [ "abfahrplan-generate.service" ];
      wantedBy = [ "multi-user.target" ];

      path = [
        pkgs.pmtiles
        pkgs.curl
        pkgs.coreutils
      ];

      script = ''
        set -euo pipefail
        target=${lib.escapeShellArg (toString cfg.basemap.path)}
        bbox=${lib.escapeShellArg (if cfg.bbox != null then cfg.bbox else "-180,-85,180,85")}

        # Protomaps publishes dated builds and drops old ones; there is no
        # "latest" to point at, so walk back until one answers.
        for back in $(seq 0 10); do
          day=$(date -u -d "-$back day" +%Y%m%d)
          url="https://build.protomaps.com/$day.pmtiles"
          if ! curl -sfI --max-time 30 "$url" >/dev/null; then
            continue
          fi
          echo "cutting $bbox from $url"
          pmtiles extract "$url" "$target.new" --bbox="$bbox" --maxzoom=${toString cfg.basemap.maxZoom}
          mv -f "$target.new" "$target"
          echo "basemap is $(du -h "$target" | cut -f1)"
          exit 0
        done

        echo "no Protomaps build answered in the last 10 days; keeping the basemap we have" >&2
        exit 1
      '';

      serviceConfig = {
        Type = "oneshot";
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = baseNameOf cfg.stateDir;
        WorkingDirectory = cfg.stateDir;
        # The first run downloads tens of megabytes; never at the expense of
        # anything else on the machine.
        Nice = 19;
        IOSchedulingClass = "idle";
        ReadWritePaths = [ cfg.stateDir ];
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        NoNewPrivileges = true;
        RestrictAddressFamilies = [
          "AF_INET"
          "AF_INET6"
          "AF_UNIX"
        ];
        RestrictNamespaces = true;
        LockPersonality = true;
        CapabilityBoundingSet = [ "" ];
        SystemCallArchitectures = "native";
        UMask = "0022";
      };
    };

    systemd.timers.abfahrplan-basemap = mkIf cfg.basemap.enable {
      description = "Refresh the abfahrplan basemap";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = cfg.basemap.refreshSchedule;
        RandomizedDelaySec = "12h";
        Persistent = true;
      };
    };

    systemd.timers.abfahrplan-generate = {
      description = "Check the GTFS feed for a new timetable";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = cfg.refreshSchedule;
        # Do not stampede the feed server at midnight along with everyone else.
        RandomizedDelaySec = "6h";
        Persistent = true;
      };
    };

    services.nginx = mkIf (cfg.domain != null) {
      enable = true;
      virtualHosts.${cfg.domain} = {
        forceSSL = true;
        enableACME = true;
        root = "${cfg.stateDir}/current";
        extraConfig = ''
          # nginx's mime.types predates ES modules, so maplibre-gl.mjs would go
          # out as application/octet-stream and the browser would refuse it --
          # the map loads nothing, with no error worth the name.
          types { application/javascript mjs; }

          autoindex off;
          charset utf-8;
        '';
        locations = {
          "/" = {
            index = "index.html";
            extraConfig = "expires 5m;";
          };
          # Everything below is named after the feed's hash or served out of a
          # build that is replaced wholesale, so it can be cached hard.
          "/static/".extraConfig = "expires 30d;";
          "/s/".extraConfig = "expires 1d;";
          "/basemap.pmtiles".extraConfig = "expires 30d;";
          "= /meta.json".extraConfig = "expires 1m;";
        };
      };
    };
  };
}
