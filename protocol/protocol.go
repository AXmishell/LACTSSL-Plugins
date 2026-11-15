// Package protocol 定义 LACTSSL 插件协议 v1 的线格式。
//
// 该包不依赖任何第三方库，主程序与插件仓库都直接引用它，
// 避免协议结构出现两份定义。
package protocol

// ProtocolVersion 是当前 helper 协议版本。
const ProtocolVersion = "v1"

// helper 进程的环境变量约定。
const (
	// MemoryLimitEnv 是主程序传入的常驻内存预算（字节）。
	MemoryLimitEnv = "LACTSSL_PLUGIN_MEMORY_LIMIT"
	// ProtocolEnv 是主程序传入的协议版本。
	ProtocolEnv = "LACTSSL_PLUGIN_PROTOCOL"
	// DeployFeaturesEnv 是主程序声明的部署插件特性列表（逗号分隔）。
	DeployFeaturesEnv = "LACTSSL_DEPLOY_PLUGIN_FEATURES"
)

// FeatureMergedDeployers 告知部署 helper 只输出合并后的规范目标。
const FeatureMergedDeployers = "merged-deployers-v1"
