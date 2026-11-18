//go:build ignore

// pluginctl 是插件仓库的构建与索引工具。
//
// 用法：
//
//	go run scripts/pluginctl manifest   # 生成 plugins.json（内部版本）
//	go run scripts/pluginctl manifest -version 1.3.4  # 三个合并包统一使用 tag 版本
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
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/catalog"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/notify/providers/channels"
	"github.com/AXmishell/LACTSSL-Plugins/scripts/buildver"
)

const (
	manifestPath = "plugins.json"
	// 主/次版本由人工维护；补丁号由 manifest 根据各合并包的输入内容自动递增。
	dnsBaseVer    = "0.1"
	deployBaseVer = "1.3"
	notifyBaseVer = "1.0"
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
	HideIcon      bool     `json:"hide_icon,omitempty"`
	SDK           string   `json:"sdk,omitempty"`
	// SourceHash 是该合并包输入文件的内容指纹，用于自动递增补丁版本。
	SourceHash string `json:"source_hash,omitempty"`

	// baseVersion 是主/次版本基线，只用于 manifest 计算，不写入 plugins.json。
	baseVersion string
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
	HideIcon         bool     `json:"hide_icon,omitempty"`
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
	Icons       []indexIcon   `json:"icons,omitempty"`
}

// iconFileRef 指向插件包内的图标文件。
type iconFileRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// iconAsset 是仓库 assets/icons 下的一张图标。
type iconAsset struct {
	Kind    string
	ID      string
	Ext     string
	MIME    string
	SHA256  string
	Data    []byte
	Source  string
	Aliases []string
}

// iconManifest 是 assets/icons/icons.json，按 kind 声明复用关系。
type iconManifest struct {
	Schema  int                          `json:"schema"`
	Aliases map[string]map[string]string `json:"aliases"`
}

// indexIcon 是索引中的内联图标，kind/id 为规范 ID，aliases 为历史 ID。
type indexIcon struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	MIME    string   `json:"mime"`
	SHA256  string   `json:"sha256"`
	Data    string   `json:"data"`
	Aliases []string `json:"aliases,omitempty"`
}

const (
	iconRoot     = "assets/icons"
	maxIconBytes = 64 << 10
)

var iconIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var iconMIME = map[string]string{
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".webp": "image/webp",
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
	HideIcon         bool                  `json:"hide_icon,omitempty"`
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
		cmdManifest(os.Args[2:])
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

func cmdManifest(args []string) {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	version := fs.String("version", "", "所有合并包统一使用的发布版本（tag 构建）")
	fs.Parse(args)

	entries := buildEntries()

	previous := map[string]pluginEntry{}
	if old, err := readManifest(); err == nil {
		for _, entry := range old {
			previous[entry.Kind+"|"+entry.ID] = entry
		}
	} else if !os.IsNotExist(err) {
		fail(err.Error())
	}

	for i := range entries {
		hash, err := buildver.HashPaths(".", sourcePaths(entries[i].Kind))
		if err != nil {
			fail("计算 " + entries[i].Kind + "/" + entries[i].ID + " 内容指纹失败: " + err.Error())
		}
		entries[i].SourceHash = hash
		if old, ok := previous[entries[i].Kind+"|"+entries[i].ID]; ok {
			entries[i].Version = buildver.Next(old.Version, old.SourceHash, entries[i].baseVersion, hash)
		}
	}

	if raw := strings.TrimSpace(*version); raw != "" {
		tagVersion := strings.TrimPrefix(raw, "v")
		if !versionPattern.MatchString(tagVersion) {
			fail("发布版本格式不合法: " + *version)
		}
		for i := range entries {
			entries[i].Version = tagVersion
		}
	}

	sortEntries(entries)
	out := manifestFile{Schema: 2, Plugins: entries}
	writeJSON(manifestPath, out)
	fmt.Printf("wrote %s with %d bundles\n", manifestPath, len(entries))
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([.+-][0-9A-Za-z.-]+)?$`)

// sourcePaths 返回影响某个合并包产物的输入。
// 共享目录（sdk、protocol 等）变化会同时升级三个包，因为改动会被链接进全部产物。
func sourcePaths(kind string) []string {
	paths := []string{
		"go.mod",
		"go.sum",
		"LICENSE",
		"protocol",
		"sdk",
		"i18n",
		"outbound",
		"catalog",
		"scripts/pluginctl",
		"scripts/buildver",
		"assets/icons/icons.json",
	}
	switch kind {
	case "dns":
		return append(paths, "cmd/dns-helper", "dns", "assets/icons/dns")
	case "deploy":
		return append(paths, "cmd/deploy-helper", "deploy", "assets/icons/deploy")
	case "notify":
		return append(paths, "cmd/notify-helper", "notify", "assets/icons/notify")
	}
	return paths
}

// buildEntries 生成三类合并插件包：DNS 全量、部署全量、通知全量。
// 不再按服务商拆分，减少仓库资产数量、安装文件数量与运行期进程数。
func buildEntries() []pluginEntry {
	dnsProviders := catalog.DNSProviders()
	allIDs := make([]string, 0, len(dnsProviders))
	for _, p := range dnsProviders {
		allIDs = append(allIDs, p.Value)
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
			Kind: "dns", ID: "all-native", Version: dnsBaseVer + ".0", Tag: "bundle_dns_all_native",
			Name: "DNS 插件包（全量·重构）", Description: "194 个服务商合并重构构建；已迁移 provider 使用仓库实现，其余暂时继续使用 lego。",
			Bundle: true, Providers: allIDs, HideIcon: true, SDK: "native", baseVersion: dnsBaseVer,
		},
		{
			Kind: "deploy", ID: "all", Version: deployBaseVer + ".0", Tag: "bundle_deploy_all",
			Name: "部署插件包", Description: "包含全部官方证书部署目标实现。",
			Bundle: true, Providers: deployIDs, baseVersion: deployBaseVer,
		},
		{
			Kind: "notify", ID: "all", Version: notifyBaseVer + ".0", Tag: "bundle_notify_all",
			Name: "通知插件包", Description: "包含全部官方通知渠道实现。",
			Bundle: true, Providers: notifyIDs, baseVersion: notifyBaseVer,
		},
	}
}

func sortEntries(entries []pluginEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
}

func readManifest() ([]pluginEntry, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var m manifestFile
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("解析 plugins.json 失败: %w", err)
	}
	return m.Plugins, nil
}

func loadManifest() []pluginEntry {
	entries, err := readManifest()
	if err != nil {
		fail("读取 plugins.json 失败: " + err.Error())
	}
	return entries
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

// entryProviders 返回插件包覆盖的 provider 集合；单插件包没有 providers 时用自身 ID。
func entryProviders(entry pluginEntry) []string {
	if len(entry.Providers) > 0 {
		return entry.Providers
	}
	return []string{entry.ID}
}

// loadIcons 读取 assets/icons 下的全部图标，并展开 icons.json 中的别名。
func loadIcons() (map[string]map[string]*iconAsset, error) {
	icons := map[string]map[string]*iconAsset{}
	manifest := iconManifest{}
	manifestPath := filepath.Join(iconRoot, "icons.json")
	if raw, err := os.ReadFile(manifestPath); err == nil {
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", manifestPath, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, kind := range []string{"dns", "deploy", "notify"} {
		dir := filepath.Join(iconRoot, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			mime, ok := iconMIME[ext]
			if !ok {
				return nil, fmt.Errorf("图标 %s 的格式不受支持（仅支持 svg/png/webp）", name)
			}
			id := strings.TrimSuffix(name, filepath.Ext(name))
			if !iconIDPattern.MatchString(id) {
				return nil, fmt.Errorf("图标 ID 非法: %q", id)
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("图标必须是普通文件: %s", name)
			}
			if info.Size() > maxIconBytes {
				return nil, fmt.Errorf("图标 %s 超过 %d 字节", name, maxIconBytes)
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if icons[kind] == nil {
				icons[kind] = map[string]*iconAsset{}
			}
			if _, dup := icons[kind][id]; dup {
				return nil, fmt.Errorf("图标重复: %s/%s", kind, id)
			}
			sum := sha256.Sum256(data)
			icons[kind][id] = &iconAsset{
				Kind: kind, ID: id, Ext: ext, MIME: mime,
				SHA256: hex.EncodeToString(sum[:]), Data: data,
				Source: filepath.Join(dir, name),
			}
		}
	}
	for kind, aliases := range manifest.Aliases {
		for alias, target := range aliases {
			if !iconIDPattern.MatchString(alias) {
				return nil, fmt.Errorf("图标别名非法: %q", alias)
			}
			asset, ok := icons[kind][target]
			if !ok {
				return nil, fmt.Errorf("图标别名 %s/%s 指向不存在的图标 %q", kind, alias, target)
			}
			asset.Aliases = append(asset.Aliases, alias)
		}
	}
	for _, byID := range icons {
		for _, asset := range byID {
			sort.Strings(asset.Aliases)
		}
	}
	return icons, nil
}

func mustLoadIcons() map[string]map[string]*iconAsset {
	icons, err := loadIcons()
	if err != nil {
		fail(err.Error())
	}
	return icons
}

// buildIndexIcons 只为 plugins.json 中仍存在的 provider 输出图标。
func buildIndexIcons(icons map[string]map[string]*iconAsset, entries []pluginEntry) []indexIcon {
	wanted := map[string]bool{}
	for _, entry := range entries {
		for _, provider := range entryProviders(entry) {
			wanted[entry.Kind+"|"+provider] = true
		}
	}
	out := make([]indexIcon, 0)
	for kind, byID := range icons {
		for id, asset := range byID {
			if !wanted[kind+"|"+id] {
				continue
			}
			out = append(out, indexIcon{
				Kind: kind, ID: id, MIME: asset.MIME, SHA256: asset.SHA256,
				Data:    "data:" + asset.MIME + ";base64," + base64.StdEncoding.EncodeToString(asset.Data),
				Aliases: asset.Aliases,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
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
	// Go 1.27 的 net/http 与新版 x/net/http2 的包装层存在兼容问题，
	// 使用 x/net 提供的 legacy HTTP/2 实现，避免 Azure SDK 初始化时 panic。
	if !strings.Contains(tags, "http2legacy") {
		tags += ",http2legacy"
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
		"hide_icon":      entry.HideIcon,
		"sdk":            entry.SDK,
	}
	pluginRaw, _ := json.MarshalIndent(pluginJSON, "", "  ")
	icons := mustLoadIcons()
	refs := map[string]iconFileRef{}
	files := map[string]fileEntry{
		"plugin.json": {data: append(pluginRaw, '\n'), mode: 0o644},
		name + ext:    {path: bin, mode: 0o755},
		"LICENSE":     {path: "LICENSE", mode: 0o644},
	}
	for _, provider := range entryProviders(entry) {
		asset, found := icons[entry.Kind][provider]
		if !found {
			continue
		}
		iconPath := "icons/" + provider + asset.Ext
		files[iconPath] = fileEntry{path: asset.Source, mode: 0o644}
		refs[provider] = iconFileRef{File: iconPath, SHA256: asset.SHA256}
	}
	if len(refs) > 0 {
		pluginJSON["icons"] = refs
		pluginRaw, _ = json.MarshalIndent(pluginJSON, "", "  ")
		files["plugin.json"] = fileEntry{data: append(pluginRaw, '\n'), mode: 0o644}
	}
	assetName := fmt.Sprintf("%s_%s_%s_%s.tar.gz", name, version, goos, goarch)
	assetPath := filepath.Join(outdir, assetName)
	if err := writeTarGz(assetPath, files); err != nil {
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
		HideIcon: entry.HideIcon,
		OS:       goos, Arch: goarch, Asset: assetName, SHA256: digest, Size: size,
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
		hideIcon := meta.HideIcon
		if !hideIcon {
			if entry, ok := findEntryByID(loadManifest(), meta.Kind, meta.ID); ok {
				hideIcon = entry.HideIcon
			}
		}
		plugins = append(plugins, indexPlugin{
			Kind: meta.Kind, ID: meta.ID, Name: name, Description: desc,
			Version: meta.Version, Protocol: "v1", Bundle: meta.Bundle,
			Providers: meta.Providers, VariantOf: meta.VariantOf, ConflictsWith: meta.ConflictsWith,
			HideIcon: hideIcon, SDK: meta.SDK, UncompressedSize: meta.UncompressedSize,
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
	index.Icons = buildIndexIcons(mustLoadIcons(), loadManifest())
	writeJSON(*out, index)
	fmt.Printf("wrote %s with %d plugins and %d icons\n", *out, len(index.Plugins), len(index.Icons))
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
