package atsf4g_go_robot_protocol_base

import conn "github.com/atframework/robot-go/conn"

var SocketUrl string

// ConnectFunc, when set, is used by CmdCreateUser and other connection
// creation points instead of the default conn.DialWebSocket(SocketUrl).
var ConnectFunc conn.NewConnectFunc = func() (conn.Connection, error) {
	return conn.DialWebSocket(SocketUrl)
}
