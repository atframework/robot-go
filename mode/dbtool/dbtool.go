package dbtool

import (
	"flag"
	"fmt"
	"os"

	host "github.com/atframework/atframe-utils-go/host"
	redis_interface "github.com/atframework/robot-go/redis"
	utils "github.com/atframework/robot-go/utils"
)

// RegisterFlags 注册 dbtool 模式需要的 flag
func RegisterFlags(flagSet *flag.FlagSet) {
	flagSet.String("pb-file", "", "path to .pb (FileDescriptorSet) file containing proto definitions")
	flagSet.String("record-prefix", "", "Redis key record prefix (overrides random-prefix)")
	flagSet.String("random-prefix", "", "use GetStableHostID as record prefix (true/false)")
	flagSet.Int64("redis-version", 0, "random-prefix version")
}

var tableExtractor TableExtractor = nil

func RegisterDatabaseTableExtractor(extractor TableExtractor) {
	tableExtractor = extractor
}

// 启动 dbtool
func Start(flagSet *flag.FlagSet) {
	pbFile := utils.GetFlagString(flagSet, "pb-file")
	if pbFile == "" {
		fmt.Println("dbtool mode requires --pb-file <path to .pb FileDescriptorSet>")
		os.Exit(1)
	}

	// 构建 Redis 配置
	redisCfg := redis_interface.ParseConfig(flagSet)

	// 确定 record prefix
	recordPrefix := utils.GetFlagString(flagSet, "record-prefix")
	if recordPrefix == "" {
		randomPrefix := utils.GetFlagString(flagSet, "random-prefix")
		if randomPrefix == "true" {
			version := utils.GetFlagInt32(flagSet, "redis-version")
			recordPrefix = host.GetStableHostID(version)
			fmt.Printf("Using stable host ID as record prefix: %s\n", recordPrefix)
		} else {
			recordPrefix = "default"
		}
	}
	fmt.Printf("Record prefix: %s\n", recordPrefix)

	// 加载 .pb 文件
	fmt.Printf("Loading proto descriptors from: %s\n", pbFile)
	if tableExtractor == nil {
		fmt.Println("No TableExtractor registered, need RegisterDatabaseTableExtractor() to extract table info from proto descriptors")
		os.Exit(1)
	}
	registry := NewRegistry(tableExtractor)
	if err := registry.LoadPBFile(pbFile); err != nil {
		fmt.Printf("Load pb file error: %v\n", err)
		os.Exit(1)
	}

	tables := registry.GetAllTables()
	if len(tables) == 0 {
		fmt.Println("No tables with database_table option found in the pb file")
		os.Exit(1)
	}
	fmt.Printf("Found %d table(s)\n", len(tables))

	// 连接 Redis
	fmt.Printf("Connecting to Redis: %v (cluster: %v)\n", redisCfg.Addrs, redisCfg.ClusterMode)
	client, err := redis_interface.NewClient(redisCfg)
	if err != nil {
		fmt.Printf("Connect Redis error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Println("Redis connected")

	// 启动交互式 Shell
	querier := NewQuerier(client, registry, recordPrefix)
	shell := NewShell(querier, registry)
	if err := shell.Run(); err != nil {
		fmt.Printf("Shell error: %v\n", err)
		os.Exit(1)
	}
}
