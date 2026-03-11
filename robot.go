package atsf4g_go_robot_user

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	log "github.com/atframework/atframe-utils-go/log"
	base "github.com/atframework/robot-go/base"
	robot_case "github.com/atframework/robot-go/case"
	cmd "github.com/atframework/robot-go/cmd"
	user_interface "github.com/atframework/robot-go/data"
	user_impl "github.com/atframework/robot-go/data/impl"
	utils "github.com/atframework/robot-go/utils"
)

func NewRobotFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet(
		fmt.Sprintf("%s [options...]", filepath.Base(os.Args[0])), flag.ContinueOnError)
	flagSet.String("url", "ws://localhost:7001/ws/v1", "server socket url")
	flagSet.Bool("h", false, "show help")
	flagSet.Bool("help", false, "show help")

	flagSet.String("case_file", "", "case file path")
	return flagSet
}

// flagSet Need Parse
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

	base.SocketUrl = flagSet.Lookup("url").Value.String()
	fmt.Println("URL:", base.SocketUrl)

	caseFile := flagSet.Lookup("case_file").Value.String()
	if caseFile != "" {
		err := robot_case.RunCaseFile(caseFile)
		if err != nil {
			fmt.Println("Run case file error:", err)
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
