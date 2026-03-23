package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConfig SSH连接配置
type SSHConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	KeyFile  string
}

// CommandResult 命令执行结果
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SSHClient SSH客户端
type SSHClient struct {
	config     SSHConfig
	client     *ssh.Client
	mu         sync.Mutex
	lastUsed   time.Time
	keepAlive  bool
	closeChan  chan struct{}
}

// NewSSHClient 创建SSH客户端
func NewSSHClient(config SSHConfig) (*SSHClient, error) {
	if config.Port == 0 {
		config.Port = 22
	}

	client := &SSHClient{
		config:    config,
		closeChan: make(chan struct{}),
	}

	// 初始连接
	if err := client.connect(); err != nil {
		return nil, err
	}

	return client, nil
}

// getAuthMethod 获取认证方法
func (c *SSHClient) getAuthMethod() (ssh.AuthMethod, error) {
	if c.config.KeyFile != "" {
		// 使用私钥认证
		key, err := os.ReadFile(c.config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("读取私钥文件失败: %w", err)
		}

		var signer ssh.Signer
		if c.config.Password != "" {
			// 带密码的私钥
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(c.config.Password))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	}

	if c.config.Password != "" {
		return ssh.Password(c.config.Password), nil
	}

	return nil, fmt.Errorf("需要提供密码或私钥文件")
}

// connect 建立SSH连接
func (c *SSHClient) connect() error {
	authMethod, err := c.getAuthMethod()
	if err != nil {
		return err
	}

	config := &ssh.ClientConfig{
		User: c.config.User,
		Auth: []ssh.AuthMethod{authMethod},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// 接受所有主机密钥（生产环境应使用已知主机验证）
			return nil
		},
		Timeout: 10 * time.Second,
	}

	address := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return fmt.Errorf("SSH连接失败: %w", err)
	}

	c.client = client
	c.lastUsed = time.Now()
	return nil
}

// ensureConnected 确保连接有效
func (c *SSHClient) ensureConnected() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 测试现有连接
	if c.client != nil {
		_, _, err := c.client.SendRequest("keepalive@golang.org", true, nil)
		if err == nil {
			c.lastUsed = time.Now()
			return nil
		}
		// 连接已断开，重新连接
		c.client.Close()
	}

	return c.connect()
}

// ExecuteCommand 执行命令
func (c *SSHClient) ExecuteCommand(command string) (*CommandResult, error) {
	return c.ExecuteCommandWithContext(context.Background(), command)
}

// ExecuteCommandWithContext 带上下文的命令执行
func (c *SSHClient) ExecuteCommandWithContext(ctx context.Context, command string) (*CommandResult, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 创建会话
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("创建SSH会话失败: %w", err)
	}
	defer session.Close()

	// 设置终端模式
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // 禁用回显
		ssh.TTY_OP_ISPEED: 14400, // 输入速率
		ssh.TTY_OP_OSPEED: 14400, // 输出速率
	}

	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		// 如果请求PTY失败，继续执行（某些命令可能不需要PTY）
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// 在goroutine中执行命令
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		// 上下文取消，关闭会话
		session.Signal(ssh.SIGKILL)
		return nil, ctx.Err()
	case err := <-done:
		result := &CommandResult{
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}

		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				result.ExitCode = exitErr.ExitStatus()
			} else {
				result.ExitCode = -1
				if result.Stderr == "" {
					result.Stderr = err.Error()
				}
			}
		}

		c.lastUsed = time.Now()
		return result, nil
	}
}

// TestConnection 测试连接
func (c *SSHClient) TestConnection() error {
	_, err := c.ExecuteCommand("echo 'connection test'")
	return err
}

// Close 关闭连接
func (c *SSHClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.closeChan)
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

// GetConfig 获取配置（不包含密码）
func (c *SSHClient) GetConfig() SSHConfig {
	return SSHConfig{
		Host:    c.config.Host,
		Port:    c.config.Port,
		User:    c.config.User,
		KeyFile: c.config.KeyFile,
	}
}