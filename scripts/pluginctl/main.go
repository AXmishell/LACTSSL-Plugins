//go:build ignore

// pluginctl 是插件仓库的构建与索引工具。
//
// 用法：
//
//	go run scripts/pluginctl manifest   # 生成 plugins.json
//	go run scripts/pluginctl gen        # 校验合并包清单
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
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Tag           string   `json:"tag"`
	MinHost       string   `json:"min_host,omitempty"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Bundle        bool     `json:"bundle,omitempty"`
	Providers     []string `json:"providers,omitempty"`
	VariantOf     string   `json:"variant_of,omitempty"`
	ConflictsWith []string `json:"conflicts_with,omitempty"`
	SDK           string   `json:"sdk,omitempty"`
}

type manifestFile struct {
	Schema  int           `json:"schema"`
	Plugins []pluginEntry `json:"plugins"`
}

type assetMeta struct {
	Kind             string   `json:"kind"`
	ID               string   `json:"id"`
	Version          string   `json:"version"`
	Tag              string   `json:"tag"`
	Name             string   `json:"name,omitempty"`
	Description      string   `json:"description,omitempty"`
	Bundle           bool     `json:"bundle,omitempty"`
	Providers        []string `json:"providers,omitempty"`
	VariantOf        string   `json:"variant_of,omitempty"`
	ConflictsWith    []string `json:"conflicts_with,omitempty"`
	SDK              string   `json:"sdk,omitempty"`
	OS               string   `json:"os"`
	Arch             string   `json:"arch"`
	Asset            string   `json:"asset"`
	SHA256           string   `json:"sha256"`
	Size             int64    `json:"size"`
	UncompressedSize int64    `json:"uncompressed_size,omitempty"`
}

type indexFile struct {
	Schema      int           `json:"schema"`
	Channel     string        `json:"channel"`
	Protocol    string        `json:"protocol"`
	GeneratedAt string        `json:"generated_at"`
	Plugins     []indexPlugin `json:"plugins"`
}

type indexPlugin struct {
	Kind             string                `json:"kind"`
	ID               string                `json:"id"`
	Name             string                `json:"name,omitempty"`
	Description      string                `json:"description,omitempty"`
	Version          string                `json:"version"`
	Protocol         string                `json:"protocol"`
	MinHost          string                `json:"min_host,omitempty"`
	Bundle           bool                  `json:"bundle,omitempty"`
	Providers        []string              `json:"providers,omitempty"`
	VariantOf        string                `json:"variant_of,omitempty"`
	ConflictsWith    []string              `json:"conflicts_with,omitempty"`
	SDK              string                `json:"sdk,omitempty"`
	UncompressedSize int64                 `json:"uncompressed_size,omitempty"`
	Single           bool                  `json:"single,omitempty"`
	Deprecated       bool                  `json:"deprecated,omitempty"`
	Assets           map[string]indexAsset `json:"assets"`
}

type indexAsset struct {
	URL              string `json:"url"`
	SHA256           string `json:"sha256"`
	Size             int64  `json:"size"`
	UncompressedSize int64  `json:"uncompressed_size,omitempty"`
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
	out := manifestFile{Schema: 2, Plugins: entries}
	writeJSON(manifestPath, out)
	fmt.Printf("wrote %s with %d bundles\n", manifestPath, len(entries))
}

// buildEntries 生成三类合并插件包：DNS 全量、部署全量、通知全量。
// 不再按服务商拆分，减少仓库资产数量、安装文件数量与运行期进程数。
func buildEntries() []pluginEntry {
	dnsProviders := catalog.DNSProviders()
	heavy := make(map[string]bool, len(dnsHeavyProviders))
	for _, id := range dnsHeavyProviders {
		heavy[id] = true
	}
	coreIDs := make([]string, 0, len(dnsProviders))
	heavyIDs := make([]string, 0, len(heavy))
	found := make(map[string]bool, len(heavy))
	for _, p := range dnsProviders {
		if heavy[p.Value] {
			heavyIDs = append(heavyIDs, p.Value)
			found[p.Value] = true
			continue
		}
		coreIDs = append(coreIDs, p.Value)
	}
	for _, id := range dnsHeavyProviders {
		if !found[id] {
			fail("dnsHeavyProviders 中存在未知服务商: " + id)
		}
	}
	deployIDs := make([]string, 0, 8)
	for _, p := range catalog.Deployers() {
		if p.Deprecated {
			continue
		}
		deployIDs = append(deployIDs, p.Value)
	}
	notifyIDs := channels.IDs()
	return []pluginEntry{
		{
			Kind: "dns", ID: "core", Version: defaultVer, Tag: "bundle_dns_core",
			Name: "DNS 插件包（常用）", Description: "包含 184 个常用 DNS 服务商；体积小、启动内存低。",
			Bundle: true, Providers: coreIDs, SDK: "lego",
		},
		{
			Kind: "dns", ID: "heavy", Version: defaultVer, Tag: "bundle_dns_heavy",
			Name: "DNS 插件包（大型云厂商）", Description: "包含 Google Cloud、Yandex、Sakura、Route53、Azure DNS 等 10 个依赖大型 SDK 的服务商。",
			Bundle: true, Providers: heavyIDs, SDK: "lego",
		},
		{
			Kind: "dns", ID: "core-native", Version: "0.1.0", Tag: "bundle_dns_core_native",
			Name: "DNS 插件包（常用·自研增量）", Description: "184 个常用服务商的增量自研变体；已迁移 provider 不再链接其 SDK。",
			Bundle: true, Providers: coreIDs, VariantOf: "core",
			ConflictsWith: []string{"core"}, SDK: "native",
		},
		{
			Kind: "dns", ID: "heavy-native", Version: "0.1.0", Tag: "bundle_dns_heavy_native",
			Name: "DNS 插件包（大型云厂商·自研）", Description: "10 个大型云厂商的自研实现，不依赖 lego 与上游云 SDK。",
			Bundle: true, Providers: heavyIDs, VariantOf: "heavy",
			ConflictsWith: []string{"heavy"}, SDK: "native",
		},
		{
			Kind: "deploy", ID: "all", Version: defaultVer, Tag: "bundle_deploy_all",
			Name: "部署插件包", Description: "包含全部官方证书部署目标实现。",
			Bundle: true, Providers: deployIDs,
		},
		{
			Kind: "notify", ID: "all", Version: defaultVer, Tag: "bundle_notify_all",
			Name: "通知插件包", Description: "包含全部官方通知渠道实现。",
			Bundle: true, Providers: notifyIDs,
		},
	}
}

// dnsHeavyProviders 是按单个二进制体积挑选出的重型 SDK 服务商。
// 把它们拆成独立合并包，可让常用包保持约 21 MiB、运行内存约 17 MiB；
// 两个包合计仍只有约 60 MiB，远低于逐服务商打包。
var dnsHeavyProviders = []string{
	"gcloud", "yandexcloud", "sakuracloud", "edgedns", "route53",
	"azuredns", "lightsail", "huaweicloud", "iij", "rfc2136",
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

// cmdGen 根据 plugins.json 生成 DNS 分组的 import 文件与构建标签选择器。
// 部署/通知只有一个合并包，不需要分组。
func cmdGen() {
	entries := loadManifest()
	expected := map[string]bool{}
	allTags := make([]string, 0, 4)
	for _, e := range entries {
		if e.Kind != "dns" || !e.Bundle {
			continue
		}
		groupID := strings.ReplaceAll(e.ID, "-", "_")
		tag := e.Tag
		if tag == "" {
			tag = "bundle_dns_" + groupID
		}
		allTags = append(allTags, tag)
		groupPath := filepath.Join("dns", "groups", groupID, "all.go")
		writeGenerated(groupPath, dnsGroupSource(groupID, e.Providers, e.SDK == "native"))
		expected[groupPath] = true

		selectorPath := filepath.Join("cmd", "dns-helper", "group_"+groupID+".go")
		writeGenerated(selectorPath, dnsGroupSelectorSource(groupID, tag))
		expected[selectorPath] = true
	}
	if len(allTags) == 0 {
		fail("plugins.json 中没有 DNS 合并包")
	}
	allPath := filepath.Join("cmd", "dns-helper", "group_all.go")
	writeGenerated(allPath, dnsGroupAllSource(allTags))
	expected[allPath] = true

	// 清理已删除分组留下的文件。
	if matches, _ := filepath.Glob(filepath.Join("cmd", "dns-helper", "group_*.go")); len(matches) > 0 {
		for _, match := range matches {
			if !expected[match] {
				_ = os.Remove(match)
			}
		}
	}
	if dirs, _ := filepath.Glob(filepath.Join("dns", "groups", "*")); len(dirs) > 0 {
		for _, dir := range dirs {
			found := false
			for path := range expected {
				if strings.HasPrefix(path, dir+string(os.PathSeparator)) {
					found = true
					break
				}
			}
			if !found {
				_ = os.RemoveAll(dir)
			}
		}
	}
	fmt.Printf("generated %d DNS group files\n", len(expected))
}

func writeGenerated(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fail(err.Error())
	}
}

func dnsGroupSource(id string, providers []string, native bool) string {
	// 按最终 import 路径排序，保证生成结果与 gofmt 的顺序一致。
	paths := make([]string, 0, len(providers))
	for _, provider := range providers {
		importPath := "github.com/AXmishell/LACTSSL-Plugins/dns/providers/" + provider
		if native {
			if info, err := os.Stat(filepath.Join("dns", "native", "providers", provider)); err == nil && info.IsDir() {
				importPath = "github.com/AXmishell/LACTSSL-Plugins/dns/native/providers/" + provider
			}
		}
		paths = append(paths, importPath)
	}
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString("// Code generated by scripts/pluginctl; DO NOT EDIT.\n\n")
	b.WriteString("// Package " + id + " 空导入该分组的全部 DNS 服务商。\n")
	if native {
		b.WriteString("// 已迁移的 provider 使用 dns/native，其余继续使用原 lego 包装。\n")
	}
	b.WriteString("package " + id + "\n\nimport (\n")
	for _, importPath := range paths {
		b.WriteString("\t_ \"" + importPath + "\"\n")
	}
	b.WriteString(")\n")
	return b.String()
}

func dnsGroupSelectorSource(id, tag string) string {
	return "// Code generated by scripts/pluginctl; DO NOT EDIT.\n\n" +
		"//go:build " + tag + "\n\n" +
		"package main\n\n" +
		"import _ \"github.com/AXmishell/LACTSSL-Plugins/dns/groups/" + id + "\"\n"
}

func dnsGroupAllSource(tags []string) string {
	var b strings.Builder
	b.WriteString("// Code generated by scripts/pluginctl; DO NOT EDIT.\n\n")
	b.WriteString("//go:build ")
	for i, tag := range tags {
		if i > 0 {
			b.WriteString(" && ")
		}
		b.WriteString("!" + tag)
	}
	b.WriteString("\n\npackage main\n\n")
	b.WriteString("import _ \"github.com/AXmishell/LACTSSL-Plugins/dns/all\"\n")
	return b.String()
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
	fmt.Printf("built %s (compressed=%d bytes, uncompressed=%d bytes, sha256=%s)\n", asset, meta.Size, meta.UncompressedSize, meta.SHA256)
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
	tags := entry.Tag
	if tags == "" {
		tags = "bundle_" + entry.Kind + "_all"
	}
	if !strings.Contains(tags, "nethttpomithttp2") {
		tags += ",nethttpomithttp2"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-tags", tags, "-ldflags", ldflags, "-o", bin, "./cmd/"+entry.Kind+"-helper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", assetMeta{}, fmt.Errorf("构建 %s/%s 失败: %v\n%s", entry.Kind, entry.ID, err, out)
	}
	if runtime.GOOS == goos && runtime.GOARCH == goarch {
		probeCmd := exec.Command(bin, "manifest")
		probeCmd.Env = append(os.Environ(), "LACTSSL_DEPLOY_PLUGIN_FEATURES=merged-deployers-v1")
		out, err := probeCmd.Output()
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
		if items == 0 {
			return "", assetMeta{}, fmt.Errorf("%s manifest 没有包含任何条目", name)
		}
		if entry.Bundle && len(entry.Providers) > 0 {
			got := make([]string, 0, items)
			for _, item := range probe.Providers {
				got = append(got, item.ID)
			}
			for _, item := range probe.Deployers {
				got = append(got, item.ID)
			}
			for _, item := range probe.Channels {
				got = append(got, item.ID)
			}
			sort.Strings(got)
			want := append([]string(nil), entry.Providers...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				return "", assetMeta{}, fmt.Errorf("%s manifest 条目与索引不一致: 期望 %d 个，实际 %d 个", name, len(want), len(got))
			}
		}
		if probe.Version != version {
			return "", assetMeta{}, fmt.Errorf("%s manifest 版本 %q 与索引版本 %q 不一致", name, probe.Version, version)
		}
	}

	pluginJSON := map[string]any{
		"schema":         1,
		"kind":           entry.Kind,
		"id":             entry.ID,
		"version":        version,
		"protocol":       "v1",
		"os":             goos,
		"arch":           goarch,
		"entrypoint":     name + ext,
		"bundle":         entry.Bundle,
		"providers":      entry.Providers,
		"variant_of":     entry.VariantOf,
		"conflicts_with": entry.ConflictsWith,
		"sdk":            entry.SDK,
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
	info, err := os.Stat(bin)
	if err != nil {
		return "", assetMeta{}, err
	}
	meta := assetMeta{
		Kind: entry.Kind, ID: entry.ID, Version: version, Tag: entry.Tag,
		Name: entry.Name, Description: entry.Description, Bundle: entry.Bundle, Providers: entry.Providers,
		VariantOf: entry.VariantOf, ConflictsWith: entry.ConflictsWith, SDK: entry.SDK,
		OS: goos, Arch: goarch, Asset: assetName, SHA256: digest, Size: size,
		UncompressedSize: info.Size(),
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
	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return err
	}
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
			URL:              strings.TrimRight(*baseURL, "/") + "/" + releaseTag + "/" + meta.Asset,
			SHA256:           meta.SHA256,
			Size:             meta.Size,
			UncompressedSize: meta.UncompressedSize,
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
		name, desc := meta.Name, meta.Description
		if name == "" || desc == "" {
			if def, ok := lookupMeta(meta.Kind, meta.ID); ok {
				if name == "" {
					name = def.name
				}
				if desc == "" {
					desc = def.desc
				}
			}
		}
		plugins = append(plugins, indexPlugin{
			Kind: meta.Kind, ID: meta.ID, Name: name, Description: desc,
			Version: meta.Version, Protocol: "v1", Bundle: meta.Bundle,
			Providers: meta.Providers, VariantOf: meta.VariantOf, ConflictsWith: meta.ConflictsWith,
			SDK: meta.SDK, UncompressedSize: meta.UncompressedSize,
			Single: !meta.Bundle, Assets: latest.assets,
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
