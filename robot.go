package atsf4g_go_robot_user

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/atframework/atframe-utils-go/log"
	agent "github.com/atframework/robot-go/agent"
	base "github.com/atframework/robot-go/base"
	robot_case "github.com/atframework/robot-go/case"
	_ "github.com/atframework/robot-go/cmd"
	conn "github.com/atframework/robot-go/conn"
	gatewayconn "github.com/atframework/robot-go/conn/atgateway"
	user_interface "github.com/atframework/robot-go/data"
	user_impl "github.com/atframework/robot-go/data/impl"
	utils "github.com/atframework/robot-go/utils"
	"gopkg.in/yaml.v3"
)

// stringSliceFlag 支持多次指定的 flag（如 --set KEY=VAL --set FOO=BAR）
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func NewRobotFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet(
		fmt.Sprintf("%s [options...]", filepath.Base(os.Args[0])), flag.ContinueOnError)
	flagSet.Bool("h", false, "show help")
	flagSet.Bool("help", false, "show help")

	flagSet.String("config", "", "yaml config file path")
	flagSet.String("case_file", "", "case file path")
	flagSet.Int("case_file_repeated", 1, "case file repeated time")
	flagSet.Var(&stringSliceFlag{}, "set", "set variable for case file: --set KEY=VALUE (repeatable)")

	// 链接层配置
	flagSet.String("url", "ws://localhost:7001/ws/v1", "server url")
	flagSet.String("connect-type", "websocket", "websocket, atgateway ...")
	flagSet.String("access-token", "", "atgateway Mod: access token (enables gateway protocol)")
	flagSet.String("key-exchange", "none", "atgateway Mod: ECDH key exchange: none, x25519, p256/p-256, p384/p-384, p521/p-521")
	flagSet.String("crypto", "none", "atgateway Mod: crypto algorithm list: none, xxtea, aes-128-cbc, aes-192-cbc, aes-256-cbc, aes-128-gcm, aes-192-gcm, aes-256-gcm, chacha20, chacha20-poly1305, xchacha20-poly1305")
	flagSet.String("compression", "none", "atgateway Mod: compression algorithm list: none, zstd, lz4, snappy, zlib")

	// 分布式模式
	flagSet.String("mode", "", "run mode: (empty)=standalone, agent, solo")
	flagSet.String("redis-addr", "", "Redis address for distributed mode")
	flagSet.String("redis-pwd", "", "Redis password")
	flagSet.String("master-addr", "", "Master HTTP address (agent mode)")
	flagSet.String("agent-id", "", "Agent ID (auto-generated if empty)")
	flagSet.String("agent-group", "", "Agent group ID (for group-based task distribution)")

	// 单节点压测模式
	flagSet.String("report-id", "", "report ID (solo mode, default: timestamp)")
	return flagSet
}

// LoadFlagSetFromYAML reads a flat YAML config file and writes its values into an unparsed FlagSet,
// then parses the FlagSet with the given args. Command line args have higher priority than YAML values.
// If yamlPath is empty, it tries to extract the path from -config/--config in args.
func LoadFlagSetFromYAML(flagSet *flag.FlagSet, yamlPath string, args []string) error {
	if yamlPath == "" {
		for i := 0; i < len(args); i++ {
			arg := args[i]
			if arg == "-config" || arg == "--config" {
				if i+1 < len(args) {
					yamlPath = args[i+1]
				}
				break
			}
			if after, found := strings.CutPrefix(arg, "-config="); found {
				yamlPath = after
				break
			}
			if after, found := strings.CutPrefix(arg, "--config="); found {
				yamlPath = after
				break
			}
		}
		if yamlPath == "" {
			_, err := os.Stat("config.yaml")
			if err == nil {
				yamlPath = "config.yaml"
			}
		}
	}

	if yamlPath != "" {
		fmt.Println("Load Config From: ", yamlPath)

		data, err := os.ReadFile(yamlPath)
		if err != nil {
			return fmt.Errorf("read yaml config file %s: %w", yamlPath, err)
		}
		var config map[string]interface{}
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("parse yaml config file %s: %w", yamlPath, err)
		}

		for key, value := range config {
			if key == "set" {
				// 特殊处理：set 支持 map/list/string 三种 YAML 写法
				continue
			}
			if flagSet.Lookup(key) != nil {
				flagSet.Set(key, fmt.Sprintf("%v", value))
			}
		}
		// 处理 set 变量：支持 map、list、string 三种写法
		if rawSet, ok := config["set"]; ok && flagSet.Lookup("set") != nil {
			switch v := rawSet.(type) {
			case map[string]interface{}:
				for k, val := range v {
					flagSet.Set("set", fmt.Sprintf("%s=%v", k, val))
				}
			case []interface{}:
				for _, item := range v {
					flagSet.Set("set", fmt.Sprintf("%v", item))
				}
			case string:
				if v != "" {
					flagSet.Set("set", v)
				}
			}
		}
	}

	return flagSet.Parse(args)
}

// StartRobot starts the robot. The flagSet should already be parsed (e.g., via LoadFlagSetFromYAML).
func StartRobot(flagSet *flag.FlagSet, unpack user_interface.UserReceiveUnpackFunc, createMsg user_interface.UserReceiveCreateMessageFunc) {
	if flagSet.Lookup("help").Value.String() == "true" ||
		flagSet.Lookup("h").Value.String() == "true" {
		flagSet.PrintDefaults()
		return
	}

	if unpack == nil || createMsg == nil {
		fmt.Println("unpack or createMsg function is nil")
		return
	}
	user_interface.RegisterCreateUser(user_impl.CreateUser, unpack, createMsg)

	var connectType string
	if flagSet.Lookup("connect-type").Value.String() != "" {
		connectType = flagSet.Lookup("connect-type").Value.String()
	}

	switch connectType {
	case "atgateway":
		cfg := gatewayconn.ParseGatewayConfig(flagSet)
		base.ConnectFunc = func() (conn.Connection, error) {
			return gatewayconn.DialGateway(base.Url, cfg)
		}
	}

	base.Url = flagSet.Lookup("url").Value.String()
	fmt.Println("URL:", base.Url)

	mode := flagSet.Lookup("mode").Value.String()
	switch mode {
	case "agent":
		fmt.Println("Starting in Agent mode")
		startAgent(flagSet, unpack, createMsg)
		return
	case "solo":
		fmt.Println("Starting in Solo mode (single-node stress test)")
		startSolo(flagSet)
		return
	}

	// --- Standalone 模式 ---
	caseFile := flagSet.Lookup("case_file").Value.String()
	if caseFile != "" {
		repeatedTimeString := flagSet.Lookup("case_file_repeated").Value.String()
		var repeatedTime int32 = 1
		if repeatedTimeString != "" {
			temp, err := strconv.Atoi(repeatedTimeString)
			if err != nil {
				fmt.Println("Invalid case_file_repeated value:", repeatedTimeString)
				return
			}
			repeatedTime = int32(temp)
		}
		err := robot_case.RunCaseFileStandAlone(caseFile, repeatedTime, GetSetVars(flagSet))
		if err != nil {
			fmt.Println("Run case file error:", err)
			log.CloseAllLogWriters()
			os.Exit(1)
		}
	} else {
		utils.ReadLine()
	}

	utils.StdoutLog("Closing all pending connections")
	user_interface.LogoutAllUsers()
	log.CloseAllLogWriters()
	utils.StdoutLog("Exiting....")
}

func getFlagString(fs *flag.FlagSet, name string) string {
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// GetSetVars 从已解析的 FlagSet 中提取 --set KEY=VALUE 变量并返回 map。
func GetSetVars(fs *flag.FlagSet) map[string]string {
	f := fs.Lookup("set")
	if f == nil {
		return nil
	}
	if sv, ok := f.Value.(*stringSliceFlag); ok {
		return robot_case.ParseSetFlags([]string(*sv))
	}
	return nil
}

// startAgent 以 Agent 模式启动
func startAgent(flagSet *flag.FlagSet, unpack user_interface.UserReceiveUnpackFunc, createMsg user_interface.UserReceiveCreateMessageFunc) {
	cfg := agent.AgentConfig{
		MasterAddr: getFlagString(flagSet, "master-addr"),
		RedisAddr:  getFlagString(flagSet, "redis-addr"),
		RedisPwd:   getFlagString(flagSet, "redis-pwd"),
		AgentID:    getFlagString(flagSet, "agent-id"),
		GroupID:    getFlagString(flagSet, "agent-group"),
	}
	a, err := agent.NewAgent(cfg)
	if err != nil {
		fmt.Println("Agent init error:", err)
		os.Exit(1)
	}
	if err := a.Start(); err != nil {
		fmt.Println("Agent error:", err)
		os.Exit(1)
	}
}
