package atsf4g_go_robot_cmd

import (
	utils "github.com/atframework/robot-go/utils"
)

var root *utils.CommandNode

func MutableCommandRoot() *utils.CommandNode {
	if root == nil {
		root = &utils.CommandNode{Children: make(map[string]*utils.CommandNode)}
	}
	return root
}
