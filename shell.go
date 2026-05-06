package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"ssh-server-mcp/sshclient"
)

// InteractiveShell 交互式Shell
type InteractiveShell struct {
	manager  *SSHServerManager
	current  string // 当前服务器
	termMode bool   // 终端模式
}

func NewInteractiveShell() *InteractiveShell {
	return &InteractiveShell{
		manager: NewSSHServerManager(),
	}
}

func (s *InteractiveShell) Run() {
	fmt.Println("╔════════════════════════════════════════════╗")
	fmt.Println("║       SSH Server MCP - Interactive Shell   ║")
	fmt.Println("╚════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("输入 'help' 查看可用命令, 'exit' 退出")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	for {
		// 显示提示符
		prompt := "ssh-mcp> "
		if s.current != "" {
			prompt = fmt.Sprintf("%s> ", s.current)
		}
		fmt.Printf("%s", prompt)

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\n再见!")
				return
			}
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 解析命令
		parts := strings.Fields(line)
		cmd := parts[0]
		args := parts[1:]

		switch cmd {
		case "help", "?":
			s.showHelp()
		case "exit", "quit", "q":
			fmt.Println("再见!")
			return
		case "list", "ls", "l":
			s.listServers()
		case "connect", "c":
			s.connectServer(args)
		case "disconnect", "dc":
			s.disconnectServer(args)
		case "add":
			s.addServer(args)
		case "remove", "rm":
			s.removeServer(args)
		case "exec", "e":
			s.execCommand(args)
		case "shell", "sh":
			s.startShell(args)
		case "clear", "cls":
			s.clearScreen()
		case "info":
			s.showInfo(args)
		default:
			// 如果有当前服务器，执行远程命令
			if s.current != "" {
				s.execRemoteCommand(line)
			} else {
				fmt.Printf("未知命令: %s (输入 'help' 查看帮助)\n", cmd)
			}
		}
	}
}

func (s *InteractiveShell) showHelp() {
	fmt.Print(`
可用命令:

  服务器管理:
    list, ls          列出所有服务器
    add <name> <host> <port> <user> <password/keyFile>
                      添加服务器
    remove, rm <name> 移除服务器
    connect, c <name> 连接到服务器
    disconnect, dc    断开当前连接

  命令执行:
    exec, e <cmd>     在当前服务器执行命令
    shell, sh         进入交互式终端模式

  其他:
    info [name]       显示服务器信息
    clear, cls        清屏
    help, ?           显示帮助
    exit, quit, q     退出

  快捷方式:
    连接服务器后，直接输入命令即可执行远程命令
`)
}

func (s *InteractiveShell) listServers() {
	servers := s.manager.List()
	if len(servers) == 0 {
		fmt.Println("没有已注册的服务器")
		return
	}

	fmt.Println("\n已注册的服务器:")
	fmt.Println("┌──────┬─────────────────────┬──────┬──────────┬─────────┐")
	fmt.Println("│ 名称 │ 地址                │ 端口 │ 用户     │ 状态    │")
	fmt.Println("├──────┼─────────────────────┼──────┼──────────┼─────────┤")

	for _, srv := range servers {
		status := "离线"
		if s.current == srv.Name {
			status = "当前"
		}
		fmt.Printf("│ %-4s │ %-19s │ %-4d │ %-8s │ %-7s │\n",
			srv.Name, srv.Host, srv.Port, srv.User, status)
	}
	fmt.Println("└──────┴─────────────────────┴──────┴──────────┴─────────┘")
}

func (s *InteractiveShell) connectServer(args []string) {
	if len(args) == 0 {
		fmt.Println("用法: connect <服务器名称>")
		return
	}

	name := args[0]
	if err := s.manager.TestConnection(name); err != nil {
		fmt.Printf("连接失败: %v\n", err)
		return
	}

	s.current = name
	config := s.manager.GetConfig(name)
	if config != nil {
		fmt.Printf("已连接到 %s (%s@%s:%d)\n", name, config.User, config.Host, config.Port)
	}
}

func (s *InteractiveShell) disconnectServer(args []string) {
	if s.current == "" {
		fmt.Println("当前没有连接的服务器")
		return
	}

	name := s.current
	s.current = ""
	fmt.Printf("已断开 %s\n", name)
}

func (s *InteractiveShell) addServer(args []string) {
	if len(args) < 5 {
		fmt.Println("用法: add <名称> <地址> <端口> <用户> <密码/密钥路径>")
		return
	}

	config := SSHServerConfig{
		Name: args[0],
		Host: args[1],
		User: args[4],
	}

	if port, err := parseInt(args[2]); err == nil {
		config.Port = port
	} else {
		config.Port = 22
	}

	// 判断是密码还是密钥文件
	auth := args[4]
	if len(args) > 5 {
		auth = args[5]
	}

	if strings.Contains(auth, "/") || strings.Contains(auth, "\\") || strings.HasPrefix(auth, "~") {
		config.KeyFile = auth
	} else {
		config.Password = auth
	}

	if err := s.manager.Register(config); err != nil {
		fmt.Printf("添加失败: %v\n", err)
		return
	}

	fmt.Printf("已添加服务器: %s\n", config.Name)
}

func (s *InteractiveShell) removeServer(args []string) {
	if len(args) == 0 {
		fmt.Println("用法: remove <服务器名称>")
		return
	}

	name := args[0]
	if s.current == name {
		s.current = ""
	}

	if err := s.manager.Unregister(name); err != nil {
		fmt.Printf("移除失败: %v\n", err)
		return
	}

	fmt.Printf("已移除服务器: %s\n", name)
}

func (s *InteractiveShell) execCommand(args []string) {
	if s.current == "" {
		fmt.Println("请先连接服务器: connect <名称>")
		return
	}

	if len(args) == 0 {
		fmt.Println("用法: exec <命令>")
		return
	}

	cmd := strings.Join(args, " ")
	s.execRemoteCommand(cmd)
}

func (s *InteractiveShell) execRemoteCommand(cmd string) {
	result, err := s.manager.Execute(s.current, cmd)
	if err != nil {
		fmt.Printf("执行失败: %v\n", err)
		return
	}

	if result.Stdout != "" {
		fmt.Print(result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Print(result.Stderr)
	}
	if result.Stdout == "" && result.Stderr == "" && result.ExitCode != 0 {
		fmt.Printf("退出码: %d\n", result.ExitCode)
	}
}

func (s *InteractiveShell) startShell(args []string) {
	if s.current == "" {
		fmt.Println("请先连接服务器: connect <名称>")
		return
	}

	config := s.manager.GetConfig(s.current)
	if config == nil {
		fmt.Println("服务器配置不存在")
		return
	}

	fmt.Printf("进入终端模式 (输入 ~. 退出)\n")
	fmt.Println(strings.Repeat("-", 50))

	// 启动交互式SSH会话
	s.startInteractiveSession(config)
}

func (s *InteractiveShell) startInteractiveSession(config *SSHServerConfig) {
	// 创建SSH连接
	client, err := sshclient.NewSSHClient(sshclient.SSHConfig{
		Host:     config.Host,
		Port:     config.Port,
		User:     config.User,
		Password: config.Password,
		KeyFile:  config.KeyFile,
	})
	if err != nil {
		fmt.Printf("连接失败: %v\n", err)
		return
	}
	defer client.Close()

	// 使用系统的ssh命令实现真正的终端
	sshArgs := []string{"-p", fmt.Sprintf("%d", config.Port)}
	if config.KeyFile != "" {
		sshArgs = append(sshArgs, "-i", config.KeyFile)
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", config.User, config.Host))

	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("\n会话结束: %v\n", err)
	}
}

func (s *InteractiveShell) clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func (s *InteractiveShell) showInfo(args []string) {
	name := s.current
	if len(args) > 0 {
		name = args[0]
	}

	if name == "" {
		fmt.Println("用法: info <服务器名称>")
		return
	}

	config := s.manager.GetConfig(name)
	if config == nil {
		fmt.Printf("服务器 %s 不存在\n", name)
		return
	}

	fmt.Printf("\n服务器: %s\n", name)
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("地址: %s:%d\n", config.Host, config.Port)
	fmt.Printf("用户: %s\n", config.User)
	if config.KeyFile != "" {
		fmt.Printf("认证: 密钥 (%s)\n", config.KeyFile)
	} else {
		fmt.Println("认证: 密码")
	}

	// 测试连接
	fmt.Print("状态: ")
	if err := s.manager.TestConnection(name); err != nil {
		fmt.Printf("离线 (%v)\n", err)
	} else {
		fmt.Println("在线 ✓")
	}
}

func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// RunInteractive 运行交互式Shell
func RunInteractive(configPath string) {
	shell := NewInteractiveShell()

	// 加载配置文件
	if configPath != "" {
		if err := loadConfig(configPath); err != nil {
			log.Printf("加载配置文件: %v", err)
		}
		// 共享已加载的服务器到shell的manager
		shell.manager = manager
	}

	shell.Run()
}
