package ftp

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/deploy"
	"github.com/AXmishell/LACTSSL-Plugins/deploy/util"
	"github.com/AXmishell/LACTSSL-Plugins/i18n"
	"github.com/AXmishell/LACTSSL-Plugins/protocol"
	"github.com/jlaffaye/ftp"
)

// plugin 通过 FTP / FTPS 上传证书和私钥。
type plugin struct{}

func (plugin) ID() string        { return "ftp" }
func (plugin) Aliases() []string { return nil }
func (plugin) Deploy(_ context.Context, req deploy.Request) error {
	return deployFTP(util.Certificate(req.Certificate), util.Merge(req.Auth, req.Config))
}

// Verify 只读连接并登录 FTP/FTPS，必要时列出目标目录。
func (plugin) Verify(_ context.Context, req deploy.Request) ([]protocol.VerifyCheck, error) {
	cfg := util.Merge(req.Auth, req.Config)
	loc := i18n.Normalize(req.Locale)
	host := strings.TrimSpace(cfg["host"])
	user := strings.TrimSpace(cfg["user"])
	if host == "" || user == "" {
		return ftpVerifyFailed(loc, i18n.E("service.deploy.ftp_host_user_required")), nil
	}
	if cfg["password"] == "" {
		return ftpVerifyFailed(loc, i18n.E("service.deploy.ftp_password_required")), nil
	}
	tlsMode := strings.ToLower(strings.TrimSpace(cfg["tls"]))
	if tlsMode == "" {
		tlsMode = "explicit"
	}
	port := strings.TrimSpace(cfg["port"])
	if port == "" {
		if tlsMode == "implicit" {
			port = "990"
		} else {
			port = "21"
		}
	}
	options := []ftp.DialOption{ftp.DialWithTimeout(20 * time.Second)}
	switch tlsMode {
	case "none":
	case "explicit", "implicit":
		tlsConfig := &tls.Config{ServerName: host, InsecureSkipVerify: cfg["skip_verify"] == "true"}
		if tlsMode == "implicit" {
			options = append(options, ftp.DialWithTLS(tlsConfig))
		} else {
			options = append(options, ftp.DialWithExplicitTLS(tlsConfig))
		}
	default:
		return ftpVerifyFailed(loc, i18n.E("service.deploy.ftp_tls_invalid", "mode", tlsMode)), nil
	}
	client, err := ftp.Dial(net.JoinHostPort(host, port), options...)
	if err != nil {
		return ftpVerifyFailed(loc, i18n.E("service.deploy.ftp_connect_failed", "error", err.Error())), nil
	}
	defer func() { _ = client.Quit() }()
	if err := client.Login(user, cfg["password"]); err != nil {
		return ftpVerifyFailed(loc, i18n.E("service.deploy.ftp_login_failed", "error", err.Error())), nil
	}
	checks := []protocol.VerifyCheck{
		{Key: "connect", Status: protocol.VerifyStatusOK, Message: i18n.T(loc, "service.verify.connect_ok", nil)},
		{Key: "auth", Status: protocol.VerifyStatusOK, Message: i18n.T(loc, "service.verify.auth_ok", nil)},
	}
	dir := path.Dir(strings.TrimSpace(cfg["cert_path"]))
	if strings.TrimSpace(cfg["cert_path"]) == "" || dir == "." || dir == "/" {
		return append(checks, protocol.VerifyCheck{Key: "scope", Status: protocol.VerifyStatusSkipped, Message: i18n.T(loc, "service.verify.scope_skipped", nil)}), nil
	}
	if _, err := client.List(dir); err != nil {
		checks = append(checks, protocol.VerifyCheck{Key: "scope", Status: protocol.VerifyStatusFailed, Message: i18n.T(loc, "service.verify.target_missing", map[string]any{"target": dir})})
		return checks, nil
	}
	return append(checks, protocol.VerifyCheck{Key: "scope", Status: protocol.VerifyStatusOK, Message: i18n.T(loc, "service.verify.target_ok", nil)}), nil
}

func ftpVerifyFailed(loc i18n.Locale, err error) []protocol.VerifyCheck {
	if err == nil {
		return nil
	}
	return []protocol.VerifyCheck{{Key: "verify", Status: protocol.VerifyStatusFailed, Message: i18n.T(loc, "service.verify.deploy_failed", map[string]any{"error": err.Error()})}}
}

func init() {
	deploy.MarkVerifiable("ftp")
	deploy.Register(plugin{})
}

// deployFTP 通过 FTP / FTPS 上传证书和私钥。
func deployFTP(cert *deploy.Certificate, cfg map[string]string) error {
	host := strings.TrimSpace(cfg["host"])
	user := strings.TrimSpace(cfg["user"])
	if host == "" || user == "" {
		return i18n.E("service.deploy.ftp_host_user_required")
	}
	if cfg["password"] == "" {
		return i18n.E("service.deploy.ftp_password_required")
	}

	tlsMode := strings.ToLower(strings.TrimSpace(cfg["tls"]))
	if tlsMode == "" {
		// 默认显式 TLS；明文 FTP 会把证书私钥和登录密码暴露在网络上。
		tlsMode = "explicit"
	}
	port := strings.TrimSpace(cfg["port"])
	if port == "" {
		if tlsMode == "implicit" {
			port = "990"
		} else {
			port = "21"
		}
	}
	if _, err := strconv.Atoi(port); err != nil {
		return i18n.E("service.deploy.ftp_port_invalid")
	}

	options := []ftp.DialOption{ftp.DialWithTimeout(20 * time.Second)}
	switch tlsMode {
	case "none":
	case "explicit", "implicit":
		tlsConfig := &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: cfg["skip_verify"] == "true", // 自建 FTP 服务常使用自签证书
		}
		if tlsMode == "implicit" {
			options = append(options, ftp.DialWithTLS(tlsConfig))
		} else {
			options = append(options, ftp.DialWithExplicitTLS(tlsConfig))
		}
	default:
		return i18n.E("service.deploy.ftp_tls_invalid", "mode", tlsMode)
	}

	client, err := ftp.Dial(net.JoinHostPort(host, port), options...)
	if err != nil {
		return i18n.E("service.deploy.ftp_connect_failed", "error", err.Error())
	}
	defer func() { _ = client.Quit() }()

	if err := client.Login(user, cfg["password"]); err != nil {
		return i18n.E("service.deploy.ftp_login_failed", "error", err.Error())
	}

	upload := func(remote string, data []byte) error {
		remote = strings.TrimSpace(remote)
		if remote == "" {
			return i18n.E("service.deploy.remote_path_required")
		}
		ftpMkdirAll(client, path.Dir(remote))
		tmp := remote + ".lactssl.tmp"
		if err := client.Stor(tmp, bytes.NewReader(data)); err != nil {
			return i18n.E("service.deploy.remote_create_failed", "error", err.Error())
		}
		// FTP 的 RNTO 在部分服务器上不允许覆盖已有文件，先删除旧文件。
		_ = client.Delete(remote)
		if err := client.Rename(tmp, remote); err != nil {
			_ = client.Delete(tmp)
			return i18n.E("service.deploy.remote_rename_failed", "error", err.Error())
		}
		return nil
	}

	if err := upload(cfg["cert_path"], []byte(cert.FullchainPEM)); err != nil {
		return err
	}
	if err := upload(cfg["key_path"], []byte(cert.KeyPEM)); err != nil {
		return err
	}
	return nil
}

// ftpMkdirAll 逐级创建远程目录。目录已存在时服务器会返回错误，可忽略，
// 真正的权限或路径问题会在后续写入文件时暴露。
func ftpMkdirAll(client *ftp.ServerConn, dir string) {
	dir = path.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "." || dir == "/" {
		return
	}
	current := ""
	if strings.HasPrefix(dir, "/") {
		current = "/"
	}
	for _, part := range strings.Split(strings.Trim(dir, "/"), "/") {
		if part == "" || part == "." {
			continue
		}
		switch current {
		case "":
			current = part
		case "/":
			current += part
		default:
			current += "/" + part
		}
		_ = client.MakeDir(current)
	}
}
