//go:build ignore

// pluginctl 是插件仓库的构建与索引工具。
//
// 用法：
//
//	go run scripts/pluginctl manifest   # 生成 plugins.json
//	go run scripts/pluginctl gen        # 生成 select_*.go
//	go run scripts/pluginctl matrix     # 输出 CI 矩阵 JSON
//	go run scripts/pluginctl build -kind dns -id cloudflare -version 1.0.0 -goos linux -goarch amd64
//	go run scripts/pluginctl index -assets dist -base-url https://.../releases/download -out index.json
//	go run scripts/pluginctl sign -key key.b64 -in index.json -out index.json.sig
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/catalog"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/notify/providers/channels"
)

const (
	manifestPath = "plugins.json"
	defaultVer   = "1.0.0"
)

type pluginEntry struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Tag     string `json:"tag"`
	MinHost string `json:"min_host,omitempty"`
}

type manifestFile struct {
	Schema  int           `json:"schema"`
	Plugins []pluginEntry `json:"plugins"`
}

type assetMeta struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Tag     string `json:"tag"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

type indexFile struct {
	Schema      int           `json:"schema"`
	Channel     string        `json:"channel"`
	Protocol    string        `json:"protocol"`
	GeneratedAt string        `json:"generated_at"`
	Plugins     []indexPlugin `json:"plugins"`
}

type indexPlugin struct {
	Kind        string                `json:"kind"`
	ID          string                `json:"id"`
	Name        string                `json:"name,omitempty"`
	Description string                `json:"description,omitempty"`
	Version     string                `json:"version"`
	Protocol    string                `json:"protocol"`
	MinHost     string                `json:"min_host,omitempty"`
	Single      bool                  `json:"single"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Assets      map[string]indexAsset `json:"assets"`
}

type indexAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func main() {
	if len(os.Args) < 2 {
		fail("用法: pluginctl manifest|gen|matrix|build|index|sign")
	}
	switch os.Args[1] {
	case "manifest":
		cmdManifest()
	case "gen":
		cmdGen()
	case "matrix":
		cmdMatrix()
	case "build":
		cmdBuild(os.Args[2:])
	case "index":
		cmdIndex(os.Args[2:])
	case "sign":
		cmdSign(os.Args[2:])
	default:
		fail("未知子命令: " + os.Args[1])
	}
}

func cmdManifest() {
	entries := buildEntries()
	sortEntries(entries)
	out := manifestFile{Schema: 1, Plugins: entries}
	writeJSON(manifestPath, out)
	fmt.Printf("wrote %s with %d plugins\n", manifestPath, len(entries))
}

func buildEntries() []pluginEntry {
	entries := make([]pluginEntry, 0, 224)
	for _, p := range catalog.DNSProviders() {
		entries = append(entries, pluginEntry{Kind: "dns", ID: p.Value, Version: defaultVer, Tag: "plugin_dns_" + p.Value})
	}
	for _, p := range catalog.Deployers() {
		if p.Deprecated {
			continue
		}
		entries = append(entries, pluginEntry{Kind: "deploy", ID: p.Value, Version: defaultVer, Tag: "plugin_deploy_" + p.Value})
	}
	for _, id := range channels.IDs() {
		entries = append(entries, pluginEntry{Kind: "notify", ID: id, Version: defaultVer, Tag: "plugin_notify_" + id})
	}
	return entries
}

func sortEntries(entries []pluginEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
}

func loadManifest() []pluginEntry {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		fail("读取 plugins.json 失败: " + err.Error())
	}
	var m manifestFile
	if err := json.Unmarshal(raw, &m); err != nil {
		fail("解析 plugins.json 失败: " + err.Error())
	}
	return m.Plugins
}

func cmdMatrix() {
	entries := loadManifest()
	writeJSONTo(os.Stdout, entries)
	fmt.Println()
}

type selector struct {
	imports []string
	init    []string
}

func cmdGen() {
	entries := loadManifest()
	files := map[string]selector{}
	for _, e := range entries {
		switch e.Kind {
		case "dns":
			files[filepath.Join("cmd", "dns-plugin", "select_"+e.ID+".go")] = selector{
				imports: []string{"_ \"github.com/AXmishell/LACTSSL-Plugins/dns/providers/" + e.ID + "\""},
			}
		case "deploy":
			prov, register := deployProvider(e.ID)
			if prov == "" {
				fail("未知部署目标: " + e.ID)
			}
			sel := selector{imports: []string{"_ \"github.com/AXmishell/LACTSSL-Plugins/deploy/providers/" + prov + "\""}}
			if register != "" {
				sel.imports = []string{"\"github.com/AXmishell/LACTSSL-Plugins/deploy/providers/panels\""}
				sel.init = []string{register}
			}
			files[filepath.Join("cmd", "deploy-plugin", "select_"+e.ID+".go")] = sel
		case "notify":
			files[filepath.Join("cmd", "notify-plugin", "select_"+e.ID+".go")] = selector{
				imports: []string{"\"github.com/AXmishell/LACTSSL-Plugins/notify/providers/channels\""},
				init:    []string{fmt.Sprintf("channels.RegisterByID(%q)", e.ID)},
			}
		default:
			fail("未知插件类型: " + e.Kind)
		}
	}
	for _, dir := range []string{"cmd/dns-plugin", "cmd/deploy-plugin", "cmd/notify-plugin"} {
		matches, _ := filepath.Glob(filepath.Join(dir, "select_*.go"))
		for _, match := range matches {
			if _, keep := files[match]; !keep {
				if err := os.Remove(match); err != nil {
					fail(err.Error())
				}
			}
		}
	}
	for path, sel := range files {
		writeSelector(path, sel)
	}
	fmt.Printf("generated %d selector files\n", len(files))
}

func deployProvider(id string) (provider, register string) {
	switch id {
	case "ftp":
		return "ftp", ""
	case "webhook":
		return "webhook", ""
	case "aliyun_cdn":
		return "aliyuncdn", ""
	case "qiniu":
		return "qiniu", ""
	case "baota":
		return "panels", "panels.RegisterBaota()"
	case "1panel":
		return "panels", "panels.RegisterOnePanel()"
	case "safeline":
		return "panels", "panels.RegisterSafeline()"
	default:
		return "", ""
	}
}

func writeSelector(path string, sel selector) {
	entry, ok := findEntry(path)
	_ = ok
	var buf strings.Builder
	buf.WriteString("// Code generated by scripts/pluginctl; DO NOT EDIT.\n\n")
	buf.WriteString("//go:build " + entry.Tag + "\n\n")
	buf.WriteString("package main\n\n")
	if len(sel.imports) == 1 {
		fmt.Fprintf(&buf, "import %s\n", sel.imports[0])
	} else if len(sel.imports) > 1 {
		buf.WriteString("import (\n")
		for _, imp := range sel.imports {
			buf.WriteString("\t" + imp + "\n")
		}
		buf.WriteString(")\n")
	}
	for _, line := range sel.init {
		buf.WriteString("\nfunc init() { " + line + " }\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		fail(err.Error())
	}
}

func findEntry(path string) (pluginEntry, bool) {
	entries := loadManifest()
	base := strings.TrimSuffix(filepath.Base(path), ".go")
	id := strings.TrimPrefix(base, "select_")
	for _, entry := range entries {
		if entry.ID == id && strings.Contains(path, entry.Kind+"-plugin") {
			return entry, true
		}
	}
	return pluginEntry{}, false
}

func cmdBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	kind := fs.String("kind", "", "dns|deploy|notify")
	id := fs.String("id", "", "plugin id")
	version := fs.String("version", "", "plugin version")
	goos := fs.String("goos", runtime.GOOS, "target os")
	goarch := fs.String("goarch", runtime.GOARCH, "target arch")
	outdir := fs.String("out", "dist", "output directory")
	fs.Parse(args)
	if *kind == "" || *id == "" {
		fail("build 需要 -kind 与 -id")
	}
	entry, ok := findEntryByID(loadManifest(), *kind, *id)
	if !ok {
		fail("plugins.json 中找不到插件: " + *kind + "/" + *id)
	}
	if *version == "" {
		*version = entry.Version
	}
	asset, meta, err := buildPlugin(entry, *version, *goos, *goarch, *outdir)
	if err != nil {
		fail(err.Error())
	}
	fmt.Printf("built %s (%d bytes, sha256=%s)\n", asset, meta.Size, meta.SHA256)
}

func findEntryByID(entries []pluginEntry, kind, id string) (pluginEntry, bool) {
	for _, e := range entries {
		if e.Kind == kind && e.ID == id {
			return e, true
		}
	}
	return pluginEntry{}, false
}

func buildPlugin(entry pluginEntry, version, goos, goarch, outdir string) (string, assetMeta, error) {
	name := "lactssl-" + entry.Kind + "-" + entry.ID
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return "", assetMeta{}, err
	}
	tmp, err := os.MkdirTemp(outdir, ".build-*")
	if err != nil {
		return "", assetMeta{}, err
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, name+ext)
	ldflags := "-s -w -X main.buildVersion=" + version
	cmd := exec.Command("go", "build", "-trimpath", "-tags", entry.Tag, "-ldflags", ldflags, "-o", bin, "./cmd/"+entry.Kind+"-plugin")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", assetMeta{}, fmt.Errorf("构建 %s/%s 失败: %v\n%s", entry.Kind, entry.ID, err, out)
	}
	if runtime.GOOS == goos && runtime.GOARCH == goarch {
		out, err := exec.Command(bin, "manifest").Output()
		if err != nil {
			return "", assetMeta{}, fmt.Errorf("%s manifest 校验失败: %v", name, err)
		}
		var probe struct {
			Providers []struct{ ID string } `json:"providers"`
			Deployers []struct{ ID string } `json:"deployers"`
			Channels  []struct{ ID string } `json:"channels"`
			Version   string                `json:"version"`
		}
		if err := json.Unmarshal(out, &probe); err != nil {
			return "", assetMeta{}, fmt.Errorf("%s manifest 不是合法 JSON: %v", name, err)
		}
		items := len(probe.Providers) + len(probe.Deployers) + len(probe.Channels)
		if items != 1 {
			return "", assetMeta{}, fmt.Errorf("%s manifest 应恰好包含一个条目，实际 %d", name, items)
		}
		if probe.Version != version {
			return "", assetMeta{}, fmt.Errorf("%s manifest 版本 %q 与索引版本 %q 不一致", name, probe.Version, version)
		}
	}

	pluginJSON := map[string]any{
		"schema":     1,
		"kind":       entry.Kind,
		"id":         entry.ID,
		"version":    version,
		"protocol":   "v1",
		"os":         goos,
		"arch":       goarch,
		"entrypoint": name + ext,
	}
	pluginRaw, _ := json.MarshalIndent(pluginJSON, "", "  ")
	assetName := fmt.Sprintf("%s_%s_%s_%s.tar.gz", name, version, goos, goarch)
	assetPath := filepath.Join(outdir, assetName)
	if err := writeTarGz(assetPath, map[string]fileEntry{
		"plugin.json": {data: append(pluginRaw, '\n'), mode: 0o644},
		name + ext:    {path: bin, mode: 0o755},
		"LICENSE":     {path: "LICENSE", mode: 0o644},
	}); err != nil {
		return "", assetMeta{}, err
	}
	digest, size, err := fileDigest(assetPath)
	if err != nil {
		return "", assetMeta{}, err
	}
	meta := assetMeta{
		Kind: entry.Kind, ID: entry.ID, Version: version, Tag: entry.Tag,
		OS: goos, Arch: goarch, Asset: assetName, SHA256: digest, Size: size,
	}
	metaPath := strings.TrimSuffix(assetPath, ".tar.gz") + ".meta.json"
	writeJSON(metaPath, meta)
	return assetPath, meta, nil
}

type fileEntry struct {
	path string
	data []byte
	mode int64
}

func writeTarGz(dest string, files map[string]fileEntry) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := files[name]
		data := entry.data
		if entry.path != "" {
			data, err = os.ReadFile(entry.path)
			if err != nil {
				return err
			}
		}
		header := &tar.Header{Name: name, Mode: entry.mode, Size: int64(len(data)), ModTime: time.Unix(0, 0)}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func cmdIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	assets := fs.String("assets", "dist", "directory containing *.meta.json")
	baseURL := fs.String("base-url", "", "release download base URL")
	bucket := fs.String("bucket", "", "release 名前缀：URL 为 <base-url>/<bucket>-<kind>/<asset>；留空按 <kind>-<id>-v<version>")
	prev := fs.String("prev", "", "已有 index.json，用于合并未重建的插件")
	out := fs.String("out", "index.json", "output index path")
	fs.Parse(args)
	if *baseURL == "" {
		fail("index 需要 -base-url")
	}
	metas, err := filepath.Glob(filepath.Join(*assets, "*.meta.json"))
	if err != nil {
		fail(err.Error())
	}
	type group struct {
		meta   assetMeta
		assets map[string]indexAsset
	}
	groups := map[string]map[string]*group{}
	for _, path := range metas {
		raw, err := os.ReadFile(path)
		if err != nil {
			fail(err.Error())
		}
		var meta assetMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			fail(err.Error())
		}
		key := meta.Kind + "|" + meta.ID
		if groups[key] == nil {
			groups[key] = map[string]*group{}
		}
		item := groups[key][meta.Version]
		if item == nil {
			item = &group{meta: meta, assets: map[string]indexAsset{}}
			groups[key][meta.Version] = item
		}
		releaseTag := fmt.Sprintf("%s-%s-v%s", meta.Kind, meta.ID, meta.Version)
		if *bucket != "" {
			releaseTag = *bucket + "-" + meta.Kind
		}
		item.assets[meta.OS+"/"+meta.Arch] = indexAsset{
			URL:    strings.TrimRight(*baseURL, "/") + "/" + releaseTag + "/" + meta.Asset,
			SHA256: meta.SHA256,
			Size:   meta.Size,
		}
	}
	plugins := make([]indexPlugin, 0, len(groups))
	for _, versions := range groups {
		var latest *group
		for _, item := range versions {
			if latest == nil || compareVersion(item.meta.Version, latest.meta.Version) > 0 {
				latest = item
			}
		}
		if latest == nil {
			continue
		}
		meta := latest.meta
		def, _ := lookupMeta(meta.Kind, meta.ID)
		plugins = append(plugins, indexPlugin{
			Kind: meta.Kind, ID: meta.ID, Name: def.name, Description: def.desc,
			Version: meta.Version, Protocol: "v1", Single: true, Assets: latest.assets,
		})
	}
	index := indexFile{Schema: 1, Channel: "stable", Protocol: "v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Plugins: plugins}
	if *prev != "" {
		plugins = mergeIndex(plugins, *prev)
		index.Plugins = plugins
	}
	sort.Slice(index.Plugins, func(i, j int) bool {
		a, b := index.Plugins[i], index.Plugins[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	writeJSON(*out, index)
	fmt.Printf("wrote %s with %d plugins\n", *out, len(index.Plugins))
}

// mergeIndex 把未参与本次构建的插件从旧索引沿用过来。
// 已被重新构建的插件以新结果为准，但不会把版本降级；
// plugins.json 中已删除的插件不再保留。
func mergeIndex(fresh []indexPlugin, prevPath string) []indexPlugin {
	raw, err := os.ReadFile(prevPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fail("读取旧索引失败: " + err.Error())
		}
		return fresh
	}
	var old indexFile
	if err := json.Unmarshal(raw, &old); err != nil {
		fail("解析旧索引失败: " + err.Error())
	}
	merged := map[string]indexPlugin{}
	for _, p := range fresh {
		merged[p.Kind+"|"+p.ID] = p
	}
	for _, p := range old.Plugins {
		key := p.Kind + "|" + p.ID
		if cur, ok := merged[key]; ok {
			if compareVersion(cur.Version, p.Version) < 0 {
				merged[key] = p
			}
			continue
		}
		if !inManifest(p.Kind, p.ID) {
			continue
		}
		merged[key] = p
	}
	out := make([]indexPlugin, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	return out
}

func inManifest(kind, id string) bool {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return true
	}
	var m manifestFile
	if err := json.Unmarshal(raw, &m); err != nil {
		return true
	}
	for _, e := range m.Plugins {
		if e.Kind == kind && e.ID == id {
			return true
		}
	}
	return false
}

type metaInfo struct{ name, desc string }

func lookupMeta(kind, id string) (metaInfo, bool) {
	switch kind {
	case "dns":
		if p, ok := catalog.LookupDNS(id); ok {
			return metaInfo{name: p.LocalizedName(i18n.Default), desc: p.LocalizedDesc(i18n.Default)}, true
		}
	case "deploy":
		if p, ok := catalog.LookupDeployer(id); ok {
			return metaInfo{name: p.LocalizedName(i18n.Default), desc: p.LocalizedDesc(i18n.Default)}, true
		}
	case "notify":
		return metaInfo{name: id, desc: ""}, true
	}
	return metaInfo{}, false
}

func cmdSign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", "", "base64 Ed25519 private key file")
	in := fs.String("in", "index.json", "index file")
	out := fs.String("out", "index.json.sig", "signature output")
	fs.Parse(args)
	if *keyPath == "" {
		fail("sign 需要 -key")
	}
	raw, err := os.ReadFile(*keyPath)
	if err != nil {
		fail(err.Error())
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		fail("解析私钥失败: " + err.Error())
	}
	var priv ed25519.PrivateKey
	switch len(key) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(key)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(key)
	default:
		fail("私钥长度必须是 32 或 64 字节，实际 " + strconv.Itoa(len(key)))
	}
	message, err := os.ReadFile(*in)
	if err != nil {
		fail(err.Error())
	}
	sig := ed25519.Sign(priv, message)
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Printf("wrote %s\n", *out)
}

func writeJSON(path string, v any) {
	f, err := os.Create(path)
	if err != nil {
		fail(err.Error())
	}
	defer f.Close()
	writeJSONTo(f, v)
	fmt.Fprintln(f)
}

func writeJSONTo(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fail(err.Error())
	}
}

func compareVersion(a, b string) int {
	parse := func(v string) ([3]int, string) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		pre := ""
		if idx := strings.IndexAny(v, "-+"); idx >= 0 {
			pre = v[idx+1:]
			v = v[:idx]
		}
		var out [3]int
		for i, part := range strings.SplitN(v, ".", 3) {
			out[i], _ = strconv.Atoi(part)
		}
		return out, pre
	}
	ca, pa := parse(a)
	cb, pb := parse(b)
	for i := 0; i < 3; i++ {
		if ca[i] > cb[i] {
			return 1
		}
		if ca[i] < cb[i] {
			return -1
		}
	}
	if pa == "" && pb == "" {
		return 0
	}
	if pa == "" {
		return 1
	}
	if pb == "" {
		return -1
	}
	if pa > pb {
		return 1
	}
	if pa < pb {
		return -1
	}
	return 0
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
