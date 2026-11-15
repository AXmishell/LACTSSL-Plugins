// Package deploy 提供官方部署 helper 内部的部署目标注册表。
package deploy

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/AXmishell/LACTSSL-Plugins/protocol"
)

// Certificate 是交给插件的最小证书材料，与协议结构保持一致。
type Certificate = protocol.Certificate

// Request 是一次部署请求。
type Request struct {
	Auth        map[string]string
	Config      map[string]string
	Certificate Certificate
}

// Deployer 是部署目标的统一接口。
type Deployer interface {
	// ID 是部署类型的规范标识，必须与主程序数据库中的 deploy_accounts.type 一致。
	ID() string
	// Aliases 是兼容别名。
	Aliases() []string
	// Deploy 执行一次证书部署。
	Deploy(ctx context.Context, req Request) error
}

var registry = struct {
	sync.RWMutex
	byID  map[string]Deployer
	alias map[string]string
}{
	byID:  make(map[string]Deployer),
	alias: make(map[string]string),
}

// Register 注册内置部署器。重复的 ID 或别名会直接 panic。
func Register(p Deployer) {
	if err := register(p); err != nil {
		panic(err)
	}
}

func register(p Deployer) error {
	registry.Lock()
	defer registry.Unlock()
	id := normalize(p.ID())
	if id == "" {
		return errInvalidID
	}
	if _, exists := registry.byID[id]; exists {
		return &DuplicateError{ID: id}
	}
	if _, exists := registry.alias[id]; exists {
		return &DuplicateError{ID: id}
	}
	for _, alias := range p.Aliases() {
		alias = normalize(alias)
		if alias == "" {
			continue
		}
		if _, exists := registry.byID[alias]; exists {
			return &DuplicateError{ID: alias}
		}
		if _, exists := registry.alias[alias]; exists {
			return &DuplicateError{ID: alias}
		}
	}
	registry.byID[id] = p
	for _, alias := range p.Aliases() {
		alias = normalize(alias)
		if alias != "" && alias != id {
			registry.alias[alias] = id
		}
	}
	return nil
}

// Lookup 按 ID 或别名查找部署器。
func Lookup(name string) (Deployer, bool) {
	key := normalize(name)
	if key == "" {
		return nil, false
	}
	registry.RLock()
	defer registry.RUnlock()
	if p, ok := registry.byID[key]; ok {
		return p, true
	}
	if id, ok := registry.alias[key]; ok {
		p, found := registry.byID[id]
		return p, found
	}
	return nil, false
}

// Has 判断部署器是否已注册（含别名）。
func Has(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// List 返回按 ID 排序的全部部署器。
func List() []Deployer {
	registry.RLock()
	defer registry.RUnlock()
	out := make([]Deployer, 0, len(registry.byID))
	for _, p := range registry.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// ResetForTest 清空注册表，仅供测试使用。
func ResetForTest() {
	registry.Lock()
	defer registry.Unlock()
	registry.byID = make(map[string]Deployer)
	registry.alias = make(map[string]string)
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// DuplicateError 表示部署器 ID 或别名冲突。
type DuplicateError struct{ ID string }

func (e *DuplicateError) Error() string { return "deploy plugin already registered: " + e.ID }

type registryError string

func (e registryError) Error() string { return string(e) }

const errInvalidID registryError = "deploy plugin id is empty or invalid"
