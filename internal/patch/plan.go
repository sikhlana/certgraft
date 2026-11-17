package patch

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/pem"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/sikhlana/certgraft/internal/roots"
)

const (
	debianBundle  = "etc/ssl/certs/ca-certificates.crt"
	rhelBundle    = "etc/pki/tls/certs/ca-bundle.crt"
	debianLocal   = "usr/local/share/ca-certificates"
	rhelAnchors   = "etc/pki/ca-trust/source/anchors"
	newFileMode   = 0o644
	localRootStem = "certgraft-"
)

// caFileVars are env vars that name a CA bundle; when the image sets one, the roots are appended to that file.
var caFileVars = []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "PIP_CERT", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE"}

// sitecustomize clears Python's strict X.509 flag. Its twin is images/python/sitecustomize.py in
// github.com/sikhlana/ai-context; change both together.
//
//go:embed sitecustomize.py
var sitecustomize []byte

type file struct {
	data     []byte
	mode     int64
	uid, gid int
}

// plan collects what one image's patch writes: files for the new layer, env to add, labels to set.
type plan struct {
	fsys   *fileSystem
	env    map[string]string
	roots  *roots.Set
	files  map[string]file
	addEnv []string
	labels map[string]string

	// bundle is the absolute path of the system bundle the runtimes are pointed at.
	bundle string
}

func (p *plan) empty() bool {
	return len(p.files) == 0 && len(p.addEnv) == 0 && len(p.labels) == 0
}

func (p *plan) build() error {
	node := p.env["NODE_VERSION"] != ""
	python := p.hasPython()

	if err := p.systemBundle(node || python || p.hasUserland()); err != nil {
		return err
	}

	if err := p.envFiles(); err != nil {
		return err
	}

	if err := p.java(); err != nil {
		return err
	}

	if node {
		p.setEnv("NODE_EXTRA_CA_CERTS", p.bundle)
	}

	if python {
		p.python()
	}

	return nil
}

// systemBundle appends the roots to the Debian/Alpine and RHEL bundles and puts each root where the distro's own
// tooling keeps local roots, so rebuilding the bundle in the image keeps them. With no bundle, it writes one holding
// only the roots, but only for an image with something that would read it.
func (p *plan) systemBundle(needed bool) error {
	var debian, rhel bool

	for _, b := range []string{debianBundle, rhelBundle} {
		resolved, ok := p.fsys.file(b)

		if !ok {
			continue
		}

		if err := p.appendRoots(resolved); err != nil {
			return err
		}

		if p.bundle == "" {
			p.bundle = "/" + b
		}

		debian = debian || b == debianBundle
		rhel = rhel || b == rhelBundle
	}

	if p.bundle == "" {
		if !needed {
			return nil
		}

		p.files[p.fsys.resolveParent(debianBundle)] = file{data: bytes.Join(p.roots.PEM(), nil), mode: newFileMode}
		p.bundle = "/" + debianBundle
	}

	if debian || p.fsys.exists("etc/debian_version") || p.fsys.exists("etc/alpine-release") {
		p.localRoots(debianLocal, ".crt")
	}

	if rhel || p.fsys.exists("etc/system-release") || p.fsys.exists("etc/redhat-release") {
		p.localRoots(rhelAnchors, ".pem")
	}

	return nil
}

func (p *plan) localRoots(dir, ext string) {
	for i, pemData := range p.roots.PEM() {
		name := path.Join(dir, localRootStem+strconv.Itoa(i+1)+ext)
		p.files[p.fsys.resolveParent(name)] = file{data: pemData, mode: newFileMode}
	}
}

// envFiles appends the roots to every CA bundle the image's own env names.
func (p *plan) envFiles() error {
	for _, k := range caFileVars {
		v := p.env[k]

		if v == "" {
			continue
		}

		resolved, ok := p.fsys.file(v)

		if !ok {
			continue
		}

		if _, planned := p.files[resolved]; planned {
			continue
		}

		if err := p.appendRoots(resolved); err != nil {
			return err
		}
	}

	return nil
}

// java adds the roots to every JVM trust store: the one under JAVA_HOME and any other lib/security/cacerts.
func (p *plan) java() error {
	var candidates []string

	if home := p.env["JAVA_HOME"]; home != "" {
		candidates = append(
			candidates,
			path.Join(home, "lib/security/cacerts"),
			path.Join(home, "jre/lib/security/cacerts"),
		)
	}

	for _, q := range p.fsys.paths() {
		if strings.HasSuffix(q, "/lib/security/cacerts") {
			candidates = append(candidates, q)
		}
	}

	slices.Sort(candidates)

	for _, c := range candidates {
		resolved, ok := p.fsys.file(c)

		if !ok {
			continue
		}

		if _, planned := p.files[resolved]; planned {
			continue
		}

		data, err := p.fsys.readFile(resolved)
		if err != nil {
			return err
		}

		out, err := addToTrustStore(data, p.roots)
		if err != nil {
			return &trustPointError{point: "java trust store /" + resolved, err: err}
		}

		if !bytes.Equal(out, data) {
			p.files[resolved] = p.rewritten(resolved, out)
		}
	}

	return nil
}

// python points Python at the system bundle and installs the strict-X.509 sitecustomize.py in its purelib, unless
// the image already has one there.
func (p *plan) python() {
	p.setEnv("SSL_CERT_FILE", p.bundle)
	p.setEnv("REQUESTS_CA_BUNDLE", p.bundle)
	p.setEnv("PIP_CERT", p.bundle)
	p.setEnv("UV_SYSTEM_CERTS", "1")

	dir := p.purelib()

	if dir == "" {
		return
	}

	target := path.Join(dir, "sitecustomize.py")

	if p.fsys.exists(target) {
		p.labels[LabelPythonStrict] = "skipped"

		return
	}

	p.files[p.fsys.resolveParent(target)] = file{data: sitecustomize, mode: newFileMode}
}

// purelib finds the directory Python imports sitecustomize from: the official images' /usr/local one for
// PYTHON_VERSION, else the newest site-packages (or Debian's dist-packages) the image has.
func (p *plan) purelib() string {
	if v := p.env["PYTHON_VERSION"]; v != "" {
		parts := strings.SplitN(v, ".", 3)

		if len(parts) >= 2 {
			dir := "usr/local/lib/python" + parts[0] + "." + parts[1] + "/site-packages"

			if p.fsys.isDir(dir) {
				return dir
			}
		}
	}

	var found []string

	for d := range p.fsys.dirs {
		for _, pattern := range []string{"usr/local/lib/python3.*/site-packages", "usr/lib/python3.*/site-packages"} {
			if ok, _ := path.Match(pattern, d); ok {
				found = append(found, d)
			}
		}
	}

	if len(found) > 0 {
		slices.Sort(found)

		return found[len(found)-1]
	}

	if p.fsys.isDir("usr/lib/python3/dist-packages") {
		return "usr/lib/python3/dist-packages"
	}

	return ""
}

func (p *plan) hasPython() bool {
	if p.env["PYTHON_VERSION"] != "" {
		return true
	}

	for dir := range strings.SplitSeq(p.env["PATH"], ":") {
		if _, ok := p.fsys.file(path.Join(dir, "python3")); ok && dir != "" {
			return true
		}
	}

	return false
}

// hasUserland reports whether the image has an OS userland (a shell or os-release) rather than being a lone binary
// like the Dockerfile frontend, which has nothing that would read a bundle.
func (p *plan) hasUserland() bool {
	return slices.ContainsFunc([]string{"bin/sh", "etc/os-release", "usr/lib/os-release"}, p.fsys.exists)
}

// setEnv adds k=v unless the image already sets k.
func (p *plan) setEnv(k, v string) {
	if _, set := p.env[k]; set {
		return
	}

	p.addEnv = append(p.addEnv, k+"="+v)
	p.env[k] = v
}

// appendRoots appends the roots missing from the PEM bundle at the real path p.
func (p *plan) appendRoots(resolved string) error {
	data, err := p.fsys.readFile(resolved)
	if err != nil {
		return err
	}

	out := appendPEMs(data, p.roots)

	if !bytes.Equal(out, data) {
		p.files[resolved] = p.rewritten(resolved, out)
	}

	return nil
}

func (p *plan) rewritten(resolved string, data []byte) file {
	n := p.fsys.nodes[resolved]

	return file{data: data, mode: n.mode, uid: n.uid, gid: n.gid}
}

// appendPEMs returns bundle with every root it doesn't already hold appended.
func appendPEMs(bundle []byte, set *roots.Set) []byte {
	have := map[[sha256.Size]byte]bool{}

	for rest := bundle; ; {
		var block *pem.Block

		block, rest = pem.Decode(rest)

		if block == nil {
			break
		}

		have[sha256.Sum256(block.Bytes)] = true
	}

	out := slices.Clone(bundle)

	for i, c := range set.Certificates() {
		if have[sha256.Sum256(c.Raw)] {
			continue
		}

		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}

		out = append(out, set.PEM()[i]...)
	}

	return out
}

// keepFor selects the files flatten keeps the content of: everything a trust point might rewrite.
func keepFor(env map[string]string) func(string) bool {
	extra := map[string]bool{}

	for _, k := range caFileVars {
		if v := env[k]; v != "" {
			extra[clean(v)] = true
		}
	}

	return func(p string) bool {
		base := path.Base(p)

		return strings.HasPrefix(p, "etc/ssl/") || strings.HasPrefix(p, "etc/pki/") ||
			base == "cacerts" || base == "sitecustomize.py" || extra[p]
	}
}
