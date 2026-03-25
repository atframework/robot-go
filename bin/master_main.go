// Package main 提供 Master 的独立二进制入口。
// 编译后仅需 Redis 即可启动分布式压测调度端，无需业务 protobuf 依赖。
//
//	go build -o robot-master ./bin
//	./robot-master -listen :8080 -redis-addr localhost:6379
package main

import (
	"fmt"
	"os"
	"time"

	robot "github.com/atframework/robot-go"
	"github.com/atframework/robot-go/master"
)

func main() {
	flagSet := robot.NewRobotFlagSet()
	if err := robot.LoadFlagSetFromYAML(flagSet, "", os.Args[1:]); err != nil {
		fmt.Println(err)
		return
	}

	flagSet.String("listen", ":8080", "HTTP listen address (master mode)")
	flagSet.String("report-dir", "./report", "HTML report output directory")
	flagSet.String("report-expiry", "", "auto-delete reports after duration (e.g. 168h=7d; empty=never)")

	cfg := master.MasterConfig{
		ListenAddr: flagSet.Lookup("listen").Value.String(),
		RedisAddr:  flagSet.Lookup("redis-addr").Value.String(),
		RedisPwd:   flagSet.Lookup("redis-pwd").Value.String(),
		ReportDir:  flagSet.Lookup("report-dir").Value.String(),
	}
	if cfg.ReportDir == "" {
		cfg.ReportDir = "./report"
	}
	if v := flagSet.Lookup("report-expiry").Value.String(); v != "" && v != "0" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ReportExpiry = d
		} else {
			fmt.Fprintf(os.Stderr, "invalid report-expiry %q: %v\n", v, err)
		}
	}

	m, err := master.NewMaster(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init master: %v\n", err)
		os.Exit(1)
	}
	if err := m.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "master: %v\n", err)
		os.Exit(1)
	}
}
