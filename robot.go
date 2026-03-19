package atsf4g_go_robot_user

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/atframework/atframe-utils-go/log"
	base "github.com/atframework/robot-go/base"
	robot_case "github.com/atframework/robot-go/case"
	cmd "github.com/atframework/robot-go/cmd"
	conn "github.com/atframework/robot-go/conn"
	gatewayconn "github.com/atframework/robot-go/conn/atgateway"
	user_interface "github.com/atframework/robot-go/data"
	user_impl "github.com/atframework/robot-go/data/impl"
	utils "github.com/atframework/robot-go/utils"
	"gopkg.in/yaml.v3"
)

func NewRobotFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet(
		fmt.Sprintf("%s [options...]", filepath.Base(os.Args[0])), flag.ContinueOnError)
	flagSet.Bool("h", false, "show help")
	flagSet.Bool("help", false, "show help")

	flagSet.String("config", "", "yaml config file path")
	flagSet.String("case_file", "", "case file path")
	flagSet.Int("case_file_repeated", 1, "case file repeated time")

	flagSet.String("url", "ws://localhost:7001/ws/v1", "server url")
	flagSet.String("connect-type", "websocket", "websocket, atgateway ...")
	flagSet.String("access-token", "", "atgateway Mod: access token (enables gateway protocol)")
	flagSet.String("key-exchange", "none", "atgateway Mod: ECDH key exchange: none, x25519, p256/p-256, p384/p-384, p521/p-521")
	flagSet.String("crypto", "none", "atgateway Mod: crypto algorithm list: none, xxtea, aes-128-cbc, aes-192-cbc, aes-256-cbc, aes-128-gcm, aes-192-gcm, aes-256-gcm, chacha20, chacha20-poly1305, xchacha20-poly1305")
	flagSet.String("compression", "none", "atgateway Mod: compression algorithm list: none, zstd, lz4, snappy, zlib")
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
			if flagSet.Lookup(key) != nil {
				flagSet.Set(key, fmt.Sprintf("%v", value))
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

	var connectType string
	if flagSet.Lookup("connect-type").Value.String() != "" {
		connectType = flagSet.Lookup("connect-type").Value.String()
	}

	switch connectType {
	case "websocket":
		fmt.Println("Using plain WebSocket connection")
	case "atgateway":
		fmt.Println("Using atgateway protocol connection")
		cfg := gatewayconn.ParseGatewayConfig(flagSet)
		base.ConnectFunc = func() (conn.Connection, error) {
			return gatewayconn.DialGateway(base.Url, cfg)
		}
	default:
		fmt.Println("Not Override ConnectFunc, Connect Type:", connectType)
	}

	user_interface.RegisterCreateUser(user_impl.CreateUser, unpack, createMsg)

	base.Url = flagSet.Lookup("url").Value.String()
	fmt.Println("URL:", base.Url)

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
		err := robot_case.RunCaseFile(caseFile, repeatedTime)
		if err != nil {
			fmt.Println("Run case file error:", err)
			log.CloseAllLogWriters()
			os.Exit(1)
		}
	} else {
		utils.ReadLine()
	}

	utils.StdoutLog("Closing all pending connections")
	cmd.LogoutAllUsers()
	log.CloseAllLogWriters()
	utils.StdoutLog("Exiting....")
}
