package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ssh-server-mcp/sshclient"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// SSHServerConfig SSH服务器配置
type SSHServerConfig struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	KeyFile  string `json:"keyFile,omitempty"`
}

// ConfigFile 配置文件结构
type ConfigFile struct {
	Servers []SSHServerConfig `json:"servers"`
}

// SSHServerManager SSH服务器管理器
type SSHServerManager struct {
	mu      sync.RWMutex
	servers map[string]*sshclient.SSHClient
	configs map[string]SSHServerConfig
}

// NewSSHServerManager 创建SSH服务器管理器
func NewSSHServerManager() *SSHServerManager {
	return &SSHServerManager{
		servers: make(map[string]*sshclient.SSHClient),
		configs: make(map[string]SSHServerConfig),
	}
}

// Register 注册SSH服务器
func (m *SSHServerManager) Register(config SSHServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.servers[config.Name]; exists {
		return fmt.Errorf("server %s already registered", config.Name)
	}

	client, err := sshclient.NewSSHClient(sshclient.SSHConfig{
		Host:     config.Host,
		Port:     config.Port,
		User:     config.User,
		Password: config.Password,
		KeyFile:  config.KeyFile,
	})
	if err != nil {
		return fmt.Errorf("failed to create SSH client: %w", err)
	}

	m.servers[config.Name] = client
	m.configs[config.Name] = config
	return nil
}

// Unregister 注销SSH服务器
func (m *SSHServerManager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.servers[name]
	if !exists {
		return fmt.Errorf("server %s not found", name)
	}

	client.Close()
	delete(m.servers, name)
	delete(m.configs, name)
	return nil
}

// Execute 在指定服务器上执行命令
func (m *SSHServerManager) Execute(name string, command string) (*sshclient.CommandResult, error) {
	m.mu.RLock()
	client, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not found", name)
	}

	return client.ExecuteCommand(command)
}

// ExecuteWithContext 带上下文的命令执行（支持超时控制）
func (m *SSHServerManager) ExecuteWithContext(ctx context.Context, name string, command string) (*sshclient.CommandResult, error) {
	m.mu.RLock()
	client, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not found", name)
	}

	return client.ExecuteCommandWithContext(ctx, command)
}

// List 列出所有已注册的服务器
func (m *SSHServerManager) List() []SSHServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]SSHServerConfig, 0, len(m.configs))
	for _, config := range m.configs {
		// 隐藏密码
		safeConfig := config
		safeConfig.Password = "******"
		result = append(result, safeConfig)
	}
	return result
}

// TestConnection 测试服务器连接
func (m *SSHServerManager) TestConnection(name string) error {
	m.mu.RLock()
	client, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("server %s not found", name)
	}

	return client.TestConnection()
}

// GetConfig 获取服务器配置
func (m *SSHServerManager) GetConfig(name string) *SSHServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if config, ok := m.configs[name]; ok {
		return &config
	}
	return nil
}

var manager = NewSSHServerManager()

// loadConfig 从配置文件加载SSH服务器配置（支持CSV和JSON格式）
func loadConfig(configPath string) error {
	if configPath == "" {
		return nil
	}

	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	defer file.Close()

	// 根据文件扩展名判断格式
	if strings.HasSuffix(strings.ToLower(configPath), ".csv") {
		return loadCSVConfig(file)
	}
	return loadJSONConfig(file)
}

// loadCSVConfig 加载CSV格式配置
// 格式: name,host,port,user,password,keyFile
func loadCSVConfig(file io.Reader) error {
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1 // 允许可变字段数
	reader.Comment = '#'        // 支持 # 开头的注释行

	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("解析CSV失败: %w", err)
	}

	for i, record := range records {
		if len(record) < 4 {
			log.Printf("警告: 第%d行字段不足，跳过", i+1)
			continue
		}

		config := SSHServerConfig{
			Name:     strings.TrimSpace(record[0]),
			Host:     strings.TrimSpace(record[1]),
			User:     strings.TrimSpace(record[3]),
			Password: strings.TrimSpace(record[4]),
			KeyFile:  strings.TrimSpace(record[5]),
		}

		// 解析端口
		if record[2] != "" {
			port, err := strconv.Atoi(strings.TrimSpace(record[2]))
			if err != nil {
				log.Printf("警告: 第%d行端口格式错误，使用默认端口22", i+1)
				config.Port = 22
			} else {
				config.Port = port
			}
		} else {
			config.Port = 22
		}

		if config.Name == "" || config.Host == "" || config.User == "" {
			log.Printf("警告: 第%d行缺少必需字段，跳过", i+1)
			continue
		}

		if config.Password == "" && config.KeyFile == "" {
			log.Printf("警告: 第%d行缺少密码或私钥，跳过", i+1)
			continue
		}

		if err := manager.Register(config); err != nil {
			log.Printf("警告: 注册服务器 %s 失败: %v", config.Name, err)
		} else {
			log.Printf("已加载服务器配置: %s (%s@%s:%d)", config.Name, config.User, config.Host, config.Port)
		}
	}

	return nil
}

// loadJSONConfig 加载JSON格式配置
func loadJSONConfig(file io.Reader) error {
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config ConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	for _, server := range config.Servers {
		if server.Port == 0 {
			server.Port = 22
		}
		if err := manager.Register(server); err != nil {
			log.Printf("警告: 注册服务器 %s 失败: %v", server.Name, err)
		} else {
			log.Printf("已加载服务器配置: %s (%s@%s:%d)", server.Name, server.User, server.Host, server.Port)
		}
	}

	return nil
}

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "", "SSH服务器配置文件路径 (JSON/CSV格式)")
	shellMode := flag.Bool("shell", false, "启动交互式Shell模式")
	flag.Parse()

	// 交互式Shell模式
	if *shellMode {
		RunInteractive(*configPath)
		return
	}

	// MCP服务器模式 - 加载配置文件
	if *configPath != "" {
		if err := loadConfig(*configPath); err != nil {
			log.Printf("加载配置文件失败: %v", err)
		}
	}

	s := server.NewMCPServer(
		"SSH Server MCP",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 注册工具
	registerTools(s)

	// 启动stdio模式
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func registerTools(s *server.MCPServer) {
	// 注册SSH服务器工具
	registerTool := mcp.NewTool("register_ssh_server",
		mcp.WithDescription("注册一个新的SSH服务器连接"),
		mcp.WithString("name", mcp.Required(), mcp.Description("服务器名称，用于后续引用")),
		mcp.WithString("host", mcp.Required(), mcp.Description("服务器地址")),
		mcp.WithNumber("port", mcp.Description("SSH端口，默认22")),
		mcp.WithString("user", mcp.Required(), mcp.Description("用户名")),
		mcp.WithString("password", mcp.Description("密码（与keyFile二选一）")),
		mcp.WithString("keyFile", mcp.Description("私钥文件路径（与password二选一）")),
	)

	s.AddTool(registerTool, handleRegisterServer)

	// 注销SSH服务器工具
	unregisterTool := mcp.NewTool("unregister_ssh_server",
		mcp.WithDescription("注销一个已注册的SSH服务器"),
		mcp.WithString("name", mcp.Required(), mcp.Description("要注销的服务器名称")),
	)

	s.AddTool(unregisterTool, handleUnregisterServer)

	// 列出服务器工具
	listTool := mcp.NewTool("list_ssh_servers",
		mcp.WithDescription("列出所有已注册的SSH服务器"),
	)

	s.AddTool(listTool, handleListServers)

	// 执行命令工具
	execTool := mcp.NewTool("ssh_execute",
		mcp.WithDescription("在指定的SSH服务器上执行命令。默认超时30秒，对于长时间运行的命令(如tail -f)建议使用 timeout 参数调整或使用 tail -n 100 代替。"),
		mcp.WithString("server", mcp.Required(), mcp.Description("服务器名称")),
		mcp.WithString("command", mcp.Required(), mcp.Description("要执行的命令")),
		mcp.WithNumber("timeout", mcp.Description("超时时间（秒），默认30秒，最大300秒")),
	)

	s.AddTool(execTool, handleExecuteCommand)

	// 测试连接工具
	testTool := mcp.NewTool("test_ssh_connection",
		mcp.WithDescription("测试指定SSH服务器的连接状态"),
		mcp.WithString("name", mcp.Required(), mcp.Description("服务器名称")),
	)

	s.AddTool(testTool, handleTestConnection)

	// 上传文件工具
	uploadTool := mcp.NewTool("ssh_upload_file",
		mcp.WithDescription("上传文件到远程SSH服务器。支持从本地文件路径上传或从base64编码内容上传。"),
		mcp.WithString("server", mcp.Required(), mcp.Description("服务器名称")),
		mcp.WithString("remotePath", mcp.Required(), mcp.Description("远程服务器上的目标文件路径")),
		mcp.WithString("localPath", mcp.Description("本地文件路径（与content二选一）")),
		mcp.WithString("content", mcp.Description("base64编码的文件内容（与localPath二选一）")),
	)

	s.AddTool(uploadTool, handleUploadFile)

	// 下载文件工具
	downloadTool := mcp.NewTool("ssh_download_file",
		mcp.WithDescription("从远程SSH服务器下载文件。可选择保存到本地路径，并返回base64编码的文件内容。"),
		mcp.WithString("server", mcp.Required(), mcp.Description("服务器名称")),
		mcp.WithString("remotePath", mcp.Required(), mcp.Description("远程服务器上的源文件路径")),
		mcp.WithString("localPath", mcp.Description("本地保存路径（可选，不提供则仅返回内容）")),
	)

	s.AddTool(downloadTool, handleDownloadFile)
}

func handleRegisterServer(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")
	host := request.GetString("host", "")
	user := request.GetString("user", "")
	password := request.GetString("password", "")
	keyFile := request.GetString("keyFile", "")

	port := request.GetInt("port", 22)

	if name == "" || host == "" || user == "" {
		return mcp.NewToolResultError("缺少必需参数: name, host, user"), nil
	}

	if password == "" && keyFile == "" {
		return mcp.NewToolResultError("需要提供password或keyFile"), nil
	}

	config := SSHServerConfig{
		Name:     name,
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		KeyFile:  keyFile,
	}

	if err := manager.Register(config); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("成功注册SSH服务器: %s (%s@%s:%d)", name, user, host, port)), nil
}

func handleUnregisterServer(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")
	if name == "" {
		return mcp.NewToolResultError("缺少必需参数: name"), nil
	}

	if err := manager.Unregister(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("成功注销SSH服务器: %s", name)), nil
}

func handleListServers(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	servers := manager.List()
	if len(servers) == 0 {
		return mcp.NewToolResultText("当前没有注册的SSH服务器"), nil
	}

	result, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("序列化服务器列表失败: %v", err)), nil
	}

	return mcp.NewToolResultText(string(result)), nil
}

func handleExecuteCommand(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	server := request.GetString("server", "")
	command := request.GetString("command", "")

	if server == "" || command == "" {
		return mcp.NewToolResultError("缺少必需参数: server, command"), nil
	}

	timeout := request.GetInt("timeout", 30)
	if timeout < 1 {
		timeout = 30
	}
	if timeout > 300 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result, err := manager.ExecuteWithContext(ctx, server, command)
	if err != nil {
		if err == context.DeadlineExceeded {
			return mcp.NewToolResultError(fmt.Sprintf("命令执行超时 (%d秒)，请尝试增加 timeout 参数或优化命令", timeout)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("执行命令失败: %v", err)), nil
	}

	output := struct {
		Command  string `json:"command"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
	}{
		Command:  command,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}

	outputJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("序列化结果失败: %v", err)), nil
	}

	return mcp.NewToolResultText(string(outputJSON)), nil
}

func handleTestConnection(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")
	if name == "" {
		return mcp.NewToolResultError("缺少必需参数: name"), nil
	}

	if err := manager.TestConnection(name); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("连接测试失败: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("服务器 %s 连接正常", name)), nil
}

func handleUploadFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverName := request.GetString("server", "")
	remotePath := request.GetString("remotePath", "")
	localPath := request.GetString("localPath", "")
	content := request.GetString("content", "")

	if serverName == "" || remotePath == "" {
		return mcp.NewToolResultError("缺少必需参数: server, remotePath"), nil
	}

	if localPath == "" && content == "" {
		return mcp.NewToolResultError("需要提供localPath或content参数"), nil
	}

	manager.mu.RLock()
	client, exists := manager.servers[serverName]
	manager.mu.RUnlock()

	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("服务器 %s 未找到", serverName)), nil
	}

	if localPath != "" {
		if err := client.UploadFile(localPath, remotePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("上传文件失败: %v", err)), nil
		}
	} else {
		data, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("base64解码失败: %v", err)), nil
		}
		if err := client.UploadFromBytes(remotePath, data); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("上传文件失败: %v", err)), nil
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("文件已成功上传到 %s:%s", serverName, remotePath)), nil
}

func handleDownloadFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverName := request.GetString("server", "")
	remotePath := request.GetString("remotePath", "")
	localPath := request.GetString("localPath", "")

	if serverName == "" || remotePath == "" {
		return mcp.NewToolResultError("缺少必需参数: server, remotePath"), nil
	}

	manager.mu.RLock()
	client, exists := manager.servers[serverName]
	manager.mu.RUnlock()

	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("服务器 %s 未找到", serverName)), nil
	}

	data, err := client.DownloadToBytes(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("下载文件失败: %v", err)), nil
	}

	// 如果指定了本地路径，保存文件
	if localPath != "" {
		if err := client.DownloadFile(remotePath, localPath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("保存本地文件失败: %v", err)), nil
		}
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	output := struct {
		Server     string `json:"server"`
		RemotePath string `json:"remotePath"`
		LocalPath  string `json:"localPath,omitempty"`
		Size       int    `json:"size"`
		Content    string `json:"content"`
	}{
		Server:     serverName,
		RemotePath: remotePath,
		LocalPath:  localPath,
		Size:       len(data),
		Content:    encoded,
	}

	outputJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("序列化结果失败: %v", err)), nil
	}

	return mcp.NewToolResultText(string(outputJSON)), nil
}
