package atsf4g_go_robot_util

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/chzyer/readline"

	base "github.com/atframework/robot-go/base"
)

type TaskActionCmd struct {
	base.TaskActionBase
	Fn func()
}

func (t *TaskActionCmd) HookRun() error {
	t.Fn()
	return nil
}

func (t *TaskActionCmd) Log(format string, a ...any) {
	StdoutLog(fmt.Sprintf(format, a...))
}

type CommandFunc func(base.TaskActionImpl, []string) string

type CommandNode struct {
	Children        map[string]*CommandNode
	Name            string
	FullName        string
	Func            CommandFunc
	ArgsInfo        string
	Desc            string
	DynamicComplete readline.DynamicCompleteFunc
	Timeout         time.Duration
}

var stdoutLog *log.Logger

func StdoutLog(log string) {
	stdoutLog.Println(log)
	rd := GetCurrentReadlineInstance()
	if rd != nil {
		rd.Refresh()
	}
}

func init() {
	stdoutLog = log.Default()
	var _ base.TaskActionImpl = &TaskActionCmd{}
}

func (node *CommandNode) SelfHelpString() []string {
	return []string{node.FullName, node.ArgsInfo, node.Desc}
}

func AllHelpStringInner(node *CommandNode) (ret [][]string) {
	if node.Func != nil {
		ret = append(ret, node.SelfHelpString())
	}
	for _, v := range node.Children {
		ret = append(ret, AllHelpStringInner(v)...)
	}
	return
}

func print3Cols(table [][]string) string {
	// 至少三个列宽
	width := [3]int{0, 0, 0}

	// 计算每列最大宽度（按 rune 数）
	for _, row := range table {
		for i := 0; i < 3; i++ {
			var cell string
			if i < len(row) {
				cell = row[i]
			} else {
				cell = ""
			}
			l := utf8.RuneCountInString(cell)
			if l > width[i] {
				width[i] = l
			}
		}
	}

	var builder strings.Builder

	// 打印，每列左对齐，两列之间用两个空格分隔
	format := fmt.Sprintf("%%-%ds  %%-%ds  %%-%ds\n", width[0], width[1], width[2])
	for _, row := range table {
		c0, c1, c2 := "", "", ""
		if len(row) > 0 {
			c0 = row[0]
		}
		if len(row) > 1 {
			c1 = row[1]
		}
		if len(row) > 2 {
			c2 = row[2]
		}

		builder.WriteString(fmt.Sprintf(format, c0, c1, c2))
	}
	return builder.String()
}

func AllHelpString(node *CommandNode) string {
	head := []string{"Command", "Args", "Desc"}
	ret := make([][]string, 0)
	ret = append(ret, head)
	ret = append(ret, AllHelpStringInner(node)...)
	return print3Cols(ret)
}

var root *CommandNode

func MutableCommandRoot() *CommandNode {
	if root != nil {
		return root
	}

	root = &CommandNode{Children: make(map[string]*CommandNode)}
	return root
}

func RegisterCommand(path []string, fn CommandFunc, argsInfo string, desc string,
	dynamicComplete readline.DynamicCompleteFunc, timeout time.Duration) {
	current := MutableCommandRoot()
	for _, key := range path {
		if current.Children[strings.ToLower(key)] == nil {
			current.Children[strings.ToLower(key)] = &CommandNode{
				Children: make(map[string]*CommandNode),
				Name:     key,
				FullName: current.FullName + " " + key,
			}
			current.Children[strings.ToLower(key)].Name = key
		}
		current = current.Children[strings.ToLower(key)]
	}
	current.Func = fn
	current.ArgsInfo = argsInfo
	current.Desc = desc
	current.DynamicComplete = dynamicComplete
	current.Timeout = timeout
}

func RegisterCommandDefaultTimeout(path []string, fn CommandFunc, argsInfo string, desc string,
	dynamicComplete readline.DynamicCompleteFunc) {
	current := MutableCommandRoot()
	for _, key := range path {
		if current.Children[strings.ToLower(key)] == nil {
			current.Children[strings.ToLower(key)] = &CommandNode{
				Children: make(map[string]*CommandNode),
				Name:     key,
				FullName: current.FullName + " " + key,
			}
			current.Children[strings.ToLower(key)].Name = key
		}
		current = current.Children[strings.ToLower(key)]
	}
	current.Func = fn
	current.ArgsInfo = argsInfo
	current.Desc = desc
	current.DynamicComplete = dynamicComplete
	current.Timeout = time.Duration(5) * time.Second
}

// FindCommand 根据路径查找命令节点
func FindCommand(path string) (args []string, node *CommandNode) {
	node = MutableCommandRoot()
	args = splitArgs(path)
	for {
		if len(node.Children) == 0 {
			break
		}
		if len(args) == 0 {
			// 没有参数了
			return
		}
		next, ok := node.Children[strings.ToLower(args[0])]
		if !ok {
			return
		}
		node = next
		args = args[1:]
	}
	return
}

// 构建自动补全器
func NewCompleter() *readline.PrefixCompleter {
	return buildCompleterFromNode(MutableCommandRoot(), "")
}

// 递归构建 PrefixCompleter
func buildCompleterFromNode(node *CommandNode, name string) *readline.PrefixCompleter {
	if len(node.Children) == 0 {
		if node.DynamicComplete != nil {
			return readline.PcItem(name, readline.PcItemDynamic(node.DynamicComplete))
		}
		return readline.PcItem(name)
	}
	items := []readline.PrefixCompleterInterface{}

	// 排序一下
	sortKey := make([]string, 0)
	for key := range node.Children {
		sortKey = append(sortKey, key)
	}
	sort.Slice(sortKey, func(i, j int) bool {
		return sortKey[i] < sortKey[j]
	})

	for _, child := range sortKey {
		items = append(items, buildCompleterFromNode(node.Children[child], node.Children[child].Name))
	}
	if node.DynamicComplete != nil {
		items = append(items, readline.PcItemDynamic(node.DynamicComplete))
	}
	if name == "" {
		return readline.NewPrefixCompleter(items...)
	}
	return readline.PcItem(name, items...)
}

func splitArgs(input string) []string {
	var result []string
	var current strings.Builder
	var quote rune   // 当前是否在引号内 (' 或 ")
	escaped := false // 上一个字符是否为 '\'

	for _, r := range input {
		switch {
		case escaped:
			// 转义状态下，直接写入字符
			current.WriteRune(r)
			escaped = false

		case r == '\\':
			// 遇到反斜杠，开启转义模式
			escaped = true

		case quote != 0:
			// 在引号内
			if r == quote {
				// 结束引号
				quote = 0
			} else {
				current.WriteRune(r)
			}

		case r == '"' || r == '\'':
			// 开始新的引号块
			quote = r

		case r == ' ' || r == '\t':
			// 空白分隔符（仅在非引号内生效）
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}

		default:
			current.WriteRune(r)
		}
	}

	// 收尾
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// ExecuteCommand 执行命令
func ExecuteCommand(mgr *base.TaskActionManager, rl *readline.Instance, input string) {
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return
	}
	args, node := FindCommand(input)
	if node == MutableCommandRoot() {
		if input == "help" {
			fmt.Print(AllHelpString(MutableCommandRoot()))
			return
		}
		fmt.Println("未知命令:", input)
		return
	}

	if node.Func != nil {
		taskAction := &TaskActionCmd{
			TaskActionBase: *base.NewTaskActionBase(node.Timeout, node.FullName),
		}
		taskAction.Fn = func() {
			result := node.Func(taskAction, args)
			if result != "" {
				fmt.Println(result)
			}
		}
		taskAction.TaskActionBase.Impl = taskAction
		mgr.RunTaskAction(taskAction)
		base.AwaitTask(taskAction)
	} else {
		fmt.Print(AllHelpString(node))
	}
}

func QuitCmd(base.TaskActionImpl, []string) string {
	return ""
}

func HistoryCmd(base.TaskActionImpl, []string) string {
	for _, item := range _historyManager.Items {
		fmt.Println(item)
	}
	_readlineInstance.Load().Refresh()
	return ""
}

var _readlineInstance atomic.Pointer[readline.Instance]
var _historyManager *HistoryManager

func GetCurrentReadlineInstance() *readline.Instance {
	return _readlineInstance.Load()
}

func ReadLine() {
	// 注册命令
	RegisterCommandDefaultTimeout([]string{"quit"}, QuitCmd, "", "退出", nil)
	RegisterCommandDefaultTimeout([]string{"history"}, HistoryCmd, "", "历史命令", nil)
	_historyManager = NewHistoryManager(historyFilePath, false)

	config := &readline.Config{
		Prompt:       "\033[32m»\033[0m ", // 设置提示符
		AutoComplete: NewCompleter(),      // 设置自动补全
	}

	rlIn, err := readline.NewEx(config)
	if err != nil {
		StdoutLog(fmt.Sprintf("无法创建 readline 实例: %v", err))
		return
	}

	_readlineInstance.Store(rlIn)
	defer func() {
		_readlineInstance.Store(nil)
		rlIn.Close()
	}()

	// 手动加载历史
	for _, item := range _historyManager.Items {
		rlIn.SaveHistory(item)
	}

	fmt.Println("Enter 'quit' to Exit, 'Tab' to AutoComplete")

	mgr := base.NewTaskActionManager()

	for {
		cmd, err := rlIn.Readline()
		if err != nil {
			continue
		}
		cmd = strings.TrimSpace(cmd)
		if cmd == "quit" {
			_historyManager.save()
			break
		}
		ExecuteCommand(mgr, rlIn, cmd)

		if cmd != "history" && cmd != "help" {
			_historyManager.add(cmd)
		}

		// ✅ 清空 readline 的历史缓存并重新加载去重后的历史
		rlIn.ResetHistory()
		for _, item := range _historyManager.Items {
			rlIn.SaveHistory(item)
		}
	}
}
