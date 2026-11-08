// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

var requiredDeveloperReferenceHosts = []string{
	"docs.itiquette.org", "creativecommons.org", "reneefrench.blogspot.com",
	"brailleinstitute.org", "conventionalcommits.org", "eupl.eu", "www.gnu.org",
	"img.shields.io", "developercertificate.org", "git-scm.com",
	"www.conventionalcommits.org", "sembr.org", "reuse.software",
	"keepachangelog.com", "kics.io", "go.dev", "proxy.golang.org",
	"oss-fuzz-build-logs.storage.googleapis.com",
	"www.brailleinstitute.org", "check.trivy.dev",
	"sum.golang.org", "semgrep.dev", "mise-versions.jdx.dev", "pypi.org", "nodejs.org",
	"registry.npmjs.org", "index.crates.io", "ziglang.org", "semver.org",
	"static.rust-lang.org", "static.crates.io", "files.pythonhosted.org", "tuf-repo-cdn.sigstore.dev",
	"registry-1.docker.io", "registry.fedoraproject.org", "registry.opensuse.org", "gcr.io",
	"npm.jsr.io", "endevco.github.io", "unofficial-builds.nodejs.org",
	"mise.jdx.dev", "just.systems", "api.reuse.software",
	"zig.savalione.com", "zigmirror.com", "zig.vortan.dev",
	"zig.mirror.mschae23.de", "zigmirror.hryx.net", "fs.liujiacai.net",
	"pkg.alexrp.com", "zig.chainsafe.dev", "pkg.hexops.org", "zig.linus.dev",
	"zig.karearl.com", "zig-mirror.tsimnet.eu", "ziglang.freetls.fastly.net",
	"zig.squirl.dev", "zig.bcr.ist", "pkg.earth", "zig.tilok.dev",
	"api.deps.dev", "docs.github.com", "docs.python.org", "docs.sigstore.dev",
	"docs.translatehouse.org", "slsa.dev", "www.macports.org",
	"services.gradle.org", "downloads.gradle.org",
	"12factor.net", "appstoreconnect.apple.com", "central.sonatype.com",
	"cyclonedx.org", "developer.android.com", "developers.google.com",
	"django.readthedocs.org", "docs.gitea.com", "docs.gitlab.com", "forgejo.org",
	"garagehq.deuxfleurs.fr", "grafana.com", "index.docker.io", "openbao.org",
	"opentelemetry.io", "play.google.com", "prometheus.io", "spdx.github.io",
	"www.cisa.gov", "zitadel.com",
}

func TestDeveloperNetworkDefaultsStayOnlineAndPortFree(t *testing.T) {
	type profile struct {
		Extends json.RawMessage `json:"extends"`
		Network struct {
			Block  bool              `json:"block"`
			Hosts  []json.RawMessage `json:"allow_domain"`
			Open   []int             `json:"open_port"`
			Listen []int             `json:"listen_port"`
		} `json:"network"`
	}
	read := func(name string) profile {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles", name+".jsonc"))
		if err != nil {
			t.Fatal(err)
		}
		data, err = hujson.Standardize(data)
		if err != nil {
			t.Fatal(err)
		}
		var result profile
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	var hosts func(string) []string
	hosts = func(name string) []string {
		t.Helper()
		if strings.HasPrefix(name, "nolabs-ai/") {
			return nil
		}
		p := read(name)
		var parents []string
		var single string
		if len(p.Extends) > 0 {
			if json.Unmarshal(p.Extends, &single) == nil {
				parents = []string{single}
			} else if err := json.Unmarshal(p.Extends, &parents); err != nil {
				t.Fatal(err)
			}
		}
		var result []string
		for _, parent := range parents {
			result = append(result, hosts(parent)...)
		}
		for _, entry := range p.Network.Hosts {
			var host string
			if json.Unmarshal(entry, &host) != nil {
				var rule struct{ Domain string }
				if err := json.Unmarshal(entry, &rule); err != nil {
					t.Fatal(err)
				}
				host = rule.Domain
			}
			result = append(result, host)
		}
		return result
	}
	defaults := read("developer-network")
	var defaultHosts []string
	for _, entry := range defaults.Network.Hosts {
		var host string
		if err := json.Unmarshal(entry, &host); err != nil {
			t.Fatal(err)
		}
		defaultHosts = append(defaultHosts, host)
	}
	for i := 1; i < len(defaultHosts); i++ {
		if defaultHosts[i-1] >= defaultHosts[i] {
			t.Fatalf("shared default hosts must be alphabetically sorted and unique: %q comes after %q", defaultHosts[i], defaultHosts[i-1])
		}
	}
	if defaults.Network.Block || len(defaults.Network.Open) > 0 || len(defaults.Network.Listen) > 0 {
		t.Fatal("shared domain defaults must not add raw TCP port exceptions")
	}
	for _, agent := range []string{"opencode", "codex"} {
		online := hosts(agent + "-chatgpt")
		for _, required := range []string{"github.com", "gitlab.com", "codefloe.com", "codeberg.org", "en.wikipedia.org", "stackoverflow.com", "api.osv.dev", "vuln.go.dev", "ghcr.io"} {
			if !slices.Contains(online, required) {
				t.Errorf("%s does not inherit %s", agent, required)
			}
		}
		for _, required := range requiredDeveloperReferenceHosts {
			if !slices.Contains(online, required) {
				t.Errorf("%s does not inherit exact reference host %s", agent, required)
			}
		}
		if offline := hosts(agent + "-offline"); len(offline) > 0 || !read(agent+"-offline").Network.Block {
			t.Errorf("%s offline profile inherits external host grants: %v", agent, offline)
		}
		for _, unwanted := range []string{"*", "*.github.com", "*.gitlab.com", "*.github.io", "*.blogspot.com", "*.storage.googleapis.com", "*.golang.org", "*.npmjs.org", "*.jsr.io", "*.crates.io", "*.docker.io", "*.fedoraproject.org", "*.opensuse.org", "*.gcr.io", "*.pypi.org", "*.nodejs.org", "*.semgrep.dev", "*.fastly.net"} {
			if slices.Contains(online, unwanted) {
				t.Errorf("%s unexpectedly inherits %s", agent, unwanted)
			}
		}
	}
}
