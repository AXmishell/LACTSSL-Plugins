// Package dns 定义插件侧的最小 DNS-01 接口。
//
// 该接口刻意不依赖 github.com/go-acme/lego/v4，这样自研 provider
// 可以只依赖标准库；原有 lego 包装仍然可以通过结构类型满足该接口。
package dns

// Provider 是 DNS 服务商插件的统一接口。
type Provider interface {
	// ID 是插件的规范标识，必须与主程序数据库中的 provider 值一致。
	ID() string
	// Aliases 是兼容别名。
	Aliases() []string
	// New 构造 DNS-01 challenge provider。配置通过 cfg 传入，
	// 同时 helper 仍会按 catalog 设置兼容的环境变量。
	New(cfg map[string]string) (ChallengeProvider, error)
}

// ChallengeProvider 是 DNS-01 所需的最小能力集合。
//
// 它与 lego 的 challenge.Provider 具有相同的方法签名，
// 因此原有 lego provider 无需修改即可继续使用。
type ChallengeProvider interface {
	Present(domain, token, keyAuth string) error
	CleanUp(domain, token, keyAuth string) error
}
