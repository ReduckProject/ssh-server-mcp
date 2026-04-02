package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/sftp"
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
	config    SSHConfig
	client    *ssh.Client
	mu        sync.Mutex
	lastUsed  time.Time
	keepAlive bool
	closeChan chan struct{}
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

// newSFTPClient 创建SFTP客户端
func (c *SSHClient) newSFTPClient() (*sftp.Client, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return sftp.NewClient(c.client)
}

// UploadFromBytes 将字节数据上传到远程服务器
func (c *SSHClient) UploadFromBytes(remotePath string, content []byte) error {
	sftpClient, err := c.newSFTPClient()
	if err != nil {
		return fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()

	// 确保远程目录存在
	dir := path.Dir(remotePath)
	if dir != "." && dir != "/" {
		sftpClient.MkdirAll(dir)
	}

	dstFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("创建远程文件失败: %w", err)
	}
	defer dstFile.Close()

	if _, err := dstFile.Write(content); err != nil {
		return fmt.Errorf("写入远程文件失败: %w", err)
	}

	return nil
}

// UploadFile 上传本地文件到远程服务器
func (c *SSHClient) UploadFile(localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("读取本地文件失败: %w", err)
	}
	return c.UploadFromBytes(remotePath, data)
}

// DownloadToBytes 从远程服务器下载文件到字节数组
func (c *SSHClient) DownloadToBytes(remotePath string) ([]byte, error) {
	sftpClient, err := c.newSFTPClient()
	if err != nil {
		return nil, fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()

	srcFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return nil, fmt.Errorf("打开远程文件失败: %w", err)
	}
	defer srcFile.Close()

	data, err := io.ReadAll(srcFile)
	if err != nil {
		return nil, fmt.Errorf("读取远程文件失败: %w", err)
	}

	return data, nil
}

// DownloadFile 从远程服务器下载文件到本地
func (c *SSHClient) DownloadFile(remotePath, localPath string) error {
	data, err := c.DownloadToBytes(remotePath)
	if err != nil {
		return err
	}

	// 确保本地目录存在
	dir := path.Dir(localPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建本地目录失败: %w", err)
		}
	}

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return fmt.Errorf("写入本地文件失败: %w", err)
	}

	return nil
}

// UploadDir 递归上传本地目录到远程服务器
func (c *SSHClient) UploadDir(localDir, remoteDir string) (int, int, error) {
	sftpClient, err := c.newSFTPClient()
	if err != nil {
		return 0, 0, fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()

	var fileCount, dirCount int

	err = filepath.Walk(localDir, func(localPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算相对路径
		relPath, err := filepath.Rel(localDir, localPath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		remotePath := path.Join(remoteDir, relPath)

		if info.IsDir() {
			if err := sftpClient.MkdirAll(remotePath); err != nil {
				return fmt.Errorf("创建远程目录 %s 失败: %w", remotePath, err)
			}
			dirCount++
			return nil
		}

		// 上传文件
		data, err := os.ReadFile(localPath)
		if err != nil {
			return fmt.Errorf("读取本地文件 %s 失败: %w", localPath, err)
		}

		// 确保父目录存在
		sftpClient.MkdirAll(path.Dir(remotePath))

		dstFile, err := sftpClient.Create(remotePath)
		if err != nil {
			return fmt.Errorf("创建远程文件 %s 失败: %w", remotePath, err)
		}
		defer dstFile.Close()

		if _, err := dstFile.Write(data); err != nil {
			return fmt.Errorf("写入远程文件 %s 失败: %w", remotePath, err)
		}
		fileCount++
		return nil
	})

	return fileCount, dirCount, err
}

// DownloadDir 递归下载远程目录到本地
func (c *SSHClient) DownloadDir(remoteDir, localDir string) (int, int, error) {
	sftpClient, err := c.newSFTPClient()
	if err != nil {
		return 0, 0, fmt.Errorf("创建SFTP客户端失败: %w", err)
	}
	defer sftpClient.Close()

	var fileCount, dirCount int

	walker := sftpClient.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return fileCount, dirCount, err
		}

		remotePath := walker.Path()
		relPath, err := filepath.Rel(remoteDir, remotePath)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)
		localPath := filepath.Join(localDir, relPath)

		if walker.Stat().IsDir() {
			if err := os.MkdirAll(localPath, 0755); err != nil {
				return fileCount, dirCount, fmt.Errorf("创建本地目录 %s 失败: %w", localPath, err)
			}
			dirCount++
			continue
		}

		// 下载文件
		srcFile, err := sftpClient.Open(remotePath)
		if err != nil {
			return fileCount, dirCount, fmt.Errorf("打开远程文件 %s 失败: %w", remotePath, err)
		}

		os.MkdirAll(filepath.Dir(localPath), 0755)
		dstFile, err := os.Create(localPath)
		if err != nil {
			srcFile.Close()
			return fileCount, dirCount, fmt.Errorf("创建本地文件 %s 失败: %w", localPath, err)
		}

		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			return fileCount, dirCount, fmt.Errorf("下载文件 %s 失败: %w", remotePath, err)
		}
		fileCount++
	}

	return fileCount, dirCount, nil
}
